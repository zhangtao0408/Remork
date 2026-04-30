# Remote Workspace Control Design

Date: 2026-04-30
Status: Draft for user review

## Problem

We need one local toolchain that lets a local human or Agent operate many remote servers without installing a full Agent environment on every server. Some servers may have limited internet access, large artifacts, or expensive environment setup. The remote workspace must remain the source of truth, while the local machine should still have enough files to search, inspect, edit, and reason over.

The system should support:

- A local editable working copy of a remote workspace.
- Remote-to-local sync where small files are mirrored automatically.
- Large-file placeholders by default, with explicit pull for full content.
- Explicit, reviewable write-back from local changes to the remote workspace.
- Remote command execution from the local CLI.
- Interactive remote shell sessions through the daemon.
- Watch/events for near-real-time updates, with manifest reconciliation as the correctness fallback.

## Non-Goals

- Do not install or synchronize a full Agent runtime on every remote server.
- Do not use the target project Git repository as the tool's state mechanism.
- Do not implement automatic bidirectional sync in the first version.
- Do not rely on SSH tunnels as the primary transport.
- Do not support Windows remote daemon hosts in the first version.
- Do not implement binary diff for large files in the first version.

## Core Decisions

### Authority Model

The remote workspace is the source of truth. The local workspace is an editable working copy backed by tool-owned metadata.

Local edits are allowed, but they do not automatically update the remote. Remote writes happen only through an explicit `apply` operation that includes base-version checks.

### Transport Model

The remote daemon listens on a VPN-reachable network address and port. The first version assumes the servers are already protected by VPN access. The daemon should still support conservative safety controls:

- Bind to a configured internal interface by default.
- Optional source IP allowlist.
- Optional shared token for deployments that want an extra guard.

### Large File Policy

The default large-file threshold is `128MB`.

Files at or below the threshold are normal sync candidates. Files above the threshold are represented locally as metadata files unless explicitly pulled.

Example:

```text
remote: /workspace/checkpoints/model.tar.gz
local:  /mirror/checkpoints/model.tar.gz.meta
```

The `.meta` file is JSON and contains enough information for humans and Agents to understand and fetch the file:

```json
{
  "kind": "remote-large-file",
  "remote_path": "/workspace/checkpoints/model.tar.gz",
  "size": 1200342218,
  "mtime": 1777542100,
  "hash": "sha256:...",
  "revision": "rev-...",
  "pulled": false,
  "pull_command": "remork pull host:/workspace/checkpoints/model.tar.gz"
}
```

## Components

### `remorkd`

`remorkd` runs on each remote server. It is a lightweight daemon, not a full Agent. It owns remote workspace inspection, file transfer, safe writes, command execution, PTY shell sessions, and file events.

Responsibilities:

- Serve workspace manifests.
- Stream file downloads and uploads.
- Apply local changesets after base verification.
- Execute non-interactive commands.
- Start and manage interactive PTY sessions.
- Watch workspace file changes and publish events.
- Report daemon and workspace status.

### `remork` CLI

`remork` runs locally. It is the interface used by humans, scripts, and Agents. The name is a short product name for this remote-workspace workflow and keeps the command memorable for frequent terminal use.

Responsibilities:

- Manage host and workspace configuration.
- Maintain local working copies.
- Maintain tool-owned state outside the user project Git repo.
- Perform manifest diffing and transfer planning.
- Run `sync`, `pull`, `status`, `diff`, `apply`, `restore`, `exec`, `shell`, and `watch`.
- Render progress bars and interactive prompts.

### Local Working Copy

The local working copy is a real directory that can be searched, opened in an editor, and edited by humans or Agents.

It is not treated as read-only. Instead, it has explicit write-back semantics:

- Local edits create dirty state.
- `remork diff` shows local changes against the last synced base.
- `remork apply` writes changes to the remote daemon after conflict checks.
- `remork sync` never silently overwrites dirty local files.

### Tool State

Tool state must not pollute the remote project or depend on the project's own `.git` directory.

Recommended state location:

```text
~/.remork/state/<host-id>/<workspace-id>/
  manifest.sqlite
  base/
  patches/
  transfer-cache/
  sessions/
  locks/
```

The state database records:

- Remote path.
- File kind.
- Size and mtime.
- Content hash when available.
- Remote revision.
- Local path.
- Local base hash.
- Dirty status.
- Large-file placeholder status.
- Last sync timestamp.

## API Surface

The exact wire format can be finalized during implementation, but the first version should expose a small API surface.

### Status

```text
GET /status
```

Returns daemon version, configured workspace roots, large-file threshold, platform, active sessions, and watch support.

### Manifest

```text
GET /manifest?root=<workspace>&path=<path>&recursive=true
```

Returns file metadata:

- Path.
- Type: file, directory, symlink, special.
- Size.
- Mtime.
- Hash if available or cheap enough.
- Revision.
- Large-file flag.
- Optional ignore/exclude reason.

The manifest is the correctness source for sync and reconciliation.

### Download

```text
GET /download?root=<workspace>&path=<path>
Range: bytes=<start>-<end>
```

Supports chunked download, range requests, progress reporting, and resumable transfers.

### Upload

```text
POST /upload
```

Uploads file content or chunks as part of an apply changeset. Used for large-file replacement and files that are not suitable for text patching.

### Apply

```text
POST /apply
```

Accepts a changeset that includes:

- Workspace root.
- Base manifest revision.
- Per-file base hash or base absence marker.
- Operation: create, update, delete, rename.
- Patch or uploaded content reference.
- Expected file mode when relevant.

The daemon verifies that each remote file still matches the base state before applying. If any operation conflicts, the daemon rejects the changeset or applies only when the request explicitly asks for partial apply.

First version default: all-or-nothing apply.

### Exec

```text
POST /exec
```

Runs a non-interactive command in a remote workspace.

Request fields:

- Workspace root.
- Cwd.
- Command and args.
- Environment overrides.
- Timeout.
- Stream mode.

Response stream:

- Stdout chunks.
- Stderr chunks.
- Exit code.
- Timing metadata.

### PTY Shell

```text
POST /pty/start
WS   /pty/<session-id>/stream
POST /pty/<session-id>/resize
POST /pty/<session-id>/signal
GET  /pty/sessions
POST /pty/<session-id>/close
```

PTY sessions support interactive remote programs such as shells, editors, REPLs, process monitors, and debuggers.

The daemon should support:

- Shell selection.
- Cwd selection.
- Environment overrides.
- Terminal rows and columns.
- Resize propagation.
- Ctrl-C and termination signals.
- Detach and reattach.
- Idle timeout cleanup.

First version session retention: keep detached sessions for a configurable timeout, with `30m` as the default.

### Watch Events

```text
WS /events?root=<workspace>
```

The daemon publishes workspace changes:

- `create`
- `update`
- `delete`
- `rename`
- `overflow`
- `resync_required`

Each event includes:

- Revision.
- Path.
- New path for rename.
- Type.
- Size.
- Mtime.
- Large-file flag.

Watch events are an acceleration path, not the only consistency mechanism. The CLI must reconcile with `manifest` after reconnect, overflow, or periodic intervals.

## Command Design

### Configure

```bash
remork host add lab-a --url http://10.0.0.12:7731
remork workspace add lab-a:/data/project --local ~/remote/lab-a/project
remork status lab-a:/data/project
```

### Sync

```bash
remork sync lab-a:/data/project
remork sync --watch lab-a:/data/project
remork sync --force lab-a:/data/project
remork sync --quiet lab-a:/data/project
remork sync --dry-run lab-a:/data/project
```

Default `sync` behavior:

- Fetch manifest.
- Diff remote state against local base and local working copy.
- Auto-download clean small-file updates.
- Write `.meta` for large files over `128MB`.
- Delete local files only when they are clean and deleted remotely.
- Preserve dirty files and report conflicts.
- Show progress for scan, diff, transfer, and write phases.

`sync --watch` keeps the process open, subscribes to daemon events, and applies the same sync rules incrementally. It still performs periodic manifest reconciliation.

### Pull

```bash
remork pull lab-a:/data/project/checkpoints/model.tar.gz
remork pull lab-a:/data/project/checkpoints/
remork pull --include-large lab-a:/data/project/checkpoints/
remork pull --force lab-a:/data/project/checkpoints/
remork pull --quiet lab-a:/data/project/checkpoints/
```

`pull` uses the same manifest, diff, and transfer engine as `sync`. The difference is policy:

- It targets a specific file or directory.
- It can fetch full large-file content.
- It prompts before large downloads and overwrites.
- It can run non-interactively with `--force` or fail fast with `--quiet` when confirmation would be required.

### Status

```bash
remork status lab-a:/data/project
```

Shows:

- Clean files.
- Local dirty files.
- Remote updates not yet synced.
- Conflicts.
- Large-file placeholders.
- Pulled large files.
- Pending apply changes.

### Diff

```bash
remork diff lab-a:/data/project
remork diff lab-a:/data/project/src/main.py
```

Shows local working-copy changes relative to the base from the last sync or pull.

For text files, show unified diff. For binary or large files, show metadata and replacement status.

### Apply

```bash
remork apply lab-a:/data/project
remork apply --dry-run lab-a:/data/project
remork apply --force lab-a:/data/project
```

Default apply behavior:

- Build changeset from local dirty files.
- Upload required content or patches.
- Ask daemon to verify base hashes.
- Apply atomically if all operations are valid.
- Update local base state after success.
- Leave local dirty state intact and report conflicts after failure.

`--force` should be treated carefully. First version can support force only for explicit whole-file replacement, with clear output that remote changes may be overwritten.

### Restore

```bash
remork restore lab-a:/data/project/src/main.py
remork restore --all lab-a:/data/project
```

Discards local dirty changes and restores files from local base or remote if the base content is not cached locally.

### Exec

```bash
remork exec lab-a:/data/project -- pytest -q
remork exec --remote-only lab-a:/data/project -- nvidia-smi
remork exec --no-sync-check lab-a:/data/project -- df -h
```

Default `exec` uses safe mode:

1. Fetch remote head revision.
2. Check that local base is current.
3. Check that there are no local dirty changes.
4. If clean but stale, sync small-file updates first.
5. If dirty or conflicted, refuse to run and suggest `status`, `diff`, `apply`, or `--remote-only`.

`--remote-only` executes against the remote current state and ignores local working-copy state.

`--no-sync-check` skips preflight checks for low-risk diagnostic commands.

### Shell

```bash
remork shell lab-a:/data/project
remork shell --remote-only lab-a:/data/project
remork shell --list lab-a
remork shell --attach <session-id>
```

Default shell uses safe mode, like `exec`. It refuses to enter a workspace shell when local dirty changes exist or the local base is stale in a way that could mislead the user or Agent.

`--remote-only` starts a raw remote shell without checking the local working copy. The CLI must clearly display that local pending changes are ignored.

After a shell exits, the CLI should check whether the remote workspace revision changed and prompt the user to run `sync` if needed.

## Write and Conflict Semantics

### Local Edit

When a local file is edited, the tool marks it dirty by comparing it with the base hash recorded during sync or pull.

### Apply Update

For a file update:

1. CLI computes local change against base.
2. CLI sends base hash and patch or content reference.
3. Daemon checks remote current hash equals base hash.
4. If equal, daemon writes the update atomically.
5. If not equal, daemon rejects with conflict.

### Apply Create

For a new local file:

- If the remote path does not exist, create it.
- If the remote path exists, report conflict.

### Apply Delete

For a local delete:

- If the remote path still matches the base hash, delete it.
- If the remote path changed, report conflict.

### Large Files

Large-file placeholders are not directly editable content. Applying a `.meta` change should not modify the remote large file.

To replace a large file:

1. Run `remork pull` to materialize the file locally, or create a new local large file.
2. Edit or replace it locally.
3. Run `remork apply`.
4. CLI uploads content in chunks.
5. Daemon verifies base and atomically replaces the remote file.

## Watch and Reconciliation

`watch/events` improves freshness but cannot be the only source of truth because filesystem event streams can overflow or lose events.

The first version should follow this rule:

- Events trigger incremental sync.
- Manifest reconciliation proves correctness.

Recommended behavior:

- On watch start, fetch full manifest.
- During watch, process events in revision order.
- If revisions skip, request manifest reconcile.
- On `overflow` or `resync_required`, request manifest reconcile.
- On reconnect, request manifest reconcile.
- Periodically reconcile, with a default interval such as `60s`.

When an event touches a clean local file:

- Small file: download and update local base.
- Large file: update `.meta`.
- Delete: remove local file if clean.

When an event touches a dirty local file:

- Do not overwrite.
- Mark conflict or pending remote update.
- Show it in `status`.

## Safety Rules

- Never silently overwrite dirty local files.
- Never silently overwrite remote files during apply when the remote no longer matches the base.
- Never depend on the target project's `.git` directory for tool state.
- Do not treat watch events as final correctness proof.
- `exec` and default `shell` must run preflight checks.
- `--remote-only` must visibly tell the user that local pending changes are ignored.
- `--force` must be explicit and visible in output.

## Error Handling

### Network Loss

- `sync` and `pull` should support resumable downloads.
- `apply` should be idempotent by changeset ID.
- PTY sessions should remain attachable for the configured retention window.
- Watch reconnect must trigger manifest reconciliation.

### Daemon Restart

- CLI should detect daemon restart through status or revision discontinuity.
- Active PTY sessions may be lost; CLI should report this clearly.
- Sync state should recover through manifest reconciliation.

### Permission Errors

- Manifest should include inaccessible paths as errors when possible.
- Downloads should fail per file with clear path and reason.
- Apply should report per-operation permission failures.

### Hash Cost

For very large files, daemon may avoid full content hash during manifest scans. It can use size, mtime, inode metadata, and optional lazy hash. Apply operations that overwrite files must use a strong enough base check for the selected policy.

## MVP Scope

First version includes:

- Linux/macOS remote daemon.
- VPN-direct HTTP or WebSocket transport.
- Host and workspace configuration.
- Manifest generation.
- Small-file sync.
- `128MB` large-file threshold and `.meta` placeholders.
- Full-file pull for files and directories.
- Progress bars.
- Interactive prompts and `--force` / `--quiet`.
- Local editable working copy.
- `status`, `diff`, `apply`, and `restore`.
- Safe-mode `exec`.
- PTY-backed interactive `shell`.
- Foreground `sync --watch` or `watch`.
- Manifest reconciliation fallback.

Later versions can add:

- Local background service.
- Binary diff.
- Multi-user policy.
- mTLS.
- Rich conflict resolution UI.
- Windows daemon support.
- Remote indexing and search.
- Integration with editor extensions.

## Testing Plan

### Unit Tests

- Manifest diff planning.
- Large-file threshold classification.
- `.meta` generation.
- Dirty file detection.
- Changeset construction.
- Conflict detection rules.
- Command preflight state transitions.

### Integration Tests

- Start a test daemon against a temporary workspace.
- Run `sync`, edit local files, run `status`, `diff`, and `apply`.
- Modify remote files after sync and verify apply conflict.
- Pull large files with prompt, `--force`, and `--quiet`.
- Simulate delete, rename, and large-file update events.
- Verify watch event loss triggers manifest reconciliation.
- Run `exec` safe mode with clean, stale, dirty, and remote-only states.
- Start PTY shell, send input, resize, detach, attach, and close.

### Manual Acceptance Tests

- Use `rg` locally on a synced workspace.
- Edit a local source file and apply it to the remote.
- Confirm a remote git repo is not affected by tool metadata.
- Sync a directory containing a file over `128MB` and verify `.meta` output.
- Pull the large file and verify progress output.
- Enter `shell --remote-only`, modify a remote file, exit, and verify `sync` sees the change.
- Disconnect during a shell and reattach before timeout.

## Open Implementation Choices

These are not unresolved requirements; they are implementation details to decide during planning:

- HTTP framework and language for `remorkd`.
- Local state database format, likely SQLite.
- Exact hash policy for large files.
- Exact patch format for text files.
- Exact local mirror path layout.
- Whether the first watch command is named `watch` or only `sync --watch`.
