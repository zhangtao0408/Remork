# Remork Product V1 Design

Date: 2026-05-01
Status: Draft for user review

## Product Goal

Remork Product V1 turns the current MVP foundation into a daily-use remote workspace tool for a small group of trusted users and Agents.

The product should feel like this:

1. A user binds one local directory to one remote workspace.
2. The user works from that local directory.
3. Common commands infer the bound remote workspace from the current directory.
4. Local edits are explicit and reviewable.
5. Remote execution is easy, but not misleading when local and remote state diverge.
6. Remote deployment works on machines with no internet and no local build toolchain.

The first product version should optimize for a small internal group over VPN. It should not become a large platform, a frontend-first product, or a pile of low-level commands that users must memorize before getting value.

## Product Principles

### Workflow First

Remork commands are organized around the user's workflow, not around daemon internals.

The primary workflow is:

```bash
mkdir -p ~/remork/project-a
cd ~/remork/project-a
remork init lab-a:/data/project-a
remork sync
remork run "pytest -q"
remork status
remork apply
remork shell
```

The user should not need to understand manifests, operation logs, watch streams, base hashes, transfer plans, or remote root allowlists before using the tool.

### Current Directory As Context

After `remork init`, the local directory becomes the default context for the workspace.

Inside a bound directory, these commands should work without a host or remote path argument:

```bash
remork sync
remork status
remork diff
remork apply
remork run "make test"
remork shell
remork pull checkpoints/model.tar.gz
remork log
```

Commands may still accept explicit workspace selectors for scripts and cross-workspace operations, but the default path is "use the binding for the current directory".

### Small Command Surface

The product has three command layers:

1. Daily commands that every user must know.
2. Advanced commands that users learn when they need more control.
3. Debug and operations commands for maintainers, Agents, and incident diagnosis.

The README and CLI help should present commands in this order. Advanced and debug commands should be discoverable without occupying the first page of the product.

### Editable Working Copy, Explicit Apply

The local directory is an editable working copy. It is not a read-only mirror.

Remote remains the source of truth. Local changes do not update remote automatically. A user must run `remork apply`, and the daemon must verify base hashes before changing remote files.

This model prevents accidental writes, avoids hidden bidirectional sync behavior, and keeps command execution predictable for both humans and Agents.

### Manifest Correctness, Event Freshness

Watch events improve freshness and reduce repeated full scans, but manifest reconciliation remains the correctness source.

Any event loss, daemon restart, revision gap, reconnect, or overflow must fall back to manifest reconciliation before the CLI claims the local state is current.

### Productized CLI Before Frontend

V1 should not introduce a web UI. The product should invest first in:

- Clear defaults.
- Predictable state.
- Helpful errors.
- A clean README learning path.
- Strong command output.
- Reliable local and remote validation.

A frontend can be added later if real usage shows that command-line workflows are not enough.

## Target Users

### Human Users

Human users want to inspect, search, edit, apply, and run commands against remote workspaces without constantly logging into each server or installing local development dependencies there.

The command experience should be terse but explain next steps when blocked.

Examples:

```text
Local changes exist. Run `remork status` to inspect, `remork apply` to write them to remote, or `remork run --remote-only ...` to ignore them for this command.
```

```text
Remote file changed since your last sync: src/train.py
Local dirty file was not overwritten.
Run `remork diff src/train.py` and then `remork apply` or `remork restore src/train.py`.
```

### Agent Users

Agents need deterministic commands, parseable output, and safe defaults.

Agent-oriented behavior:

- Every command supports `--json` where structured output is useful.
- Prompts have non-interactive equivalents.
- `--quiet` never blocks waiting for confirmation.
- Conflicts return distinct non-zero exit codes.
- `remork status --json` is the main state inspection API for Agents.
- `remork doctor --json` reports environment and daemon readiness.

### Maintainers

Maintainers need to deploy `remorkd`, upgrade it, verify connectivity, inspect logs, and diagnose failures on remote hosts that may not have internet access.

Maintainer features should exist, but should not dominate the daily user experience.

## Command Layers

### Layer 1: Must Know

These are the commands a new user must learn first.

```bash
remork init <host>:<remote-path>
remork sync
remork status
remork apply
remork run <command>
remork shell
```

#### `remork init`

Initializes the current local directory as a working copy bound to a remote workspace.

Example:

```bash
mkdir -p ~/remork/project-a
cd ~/remork/project-a
remork init remork-host-a:/tmp/remork-e2e
```

Behavior:

- Resolves `remork-host-a` from Remork host config.
- Stores a local binding for the current directory.
- Verifies daemon reachability.
- Verifies the remote root is allowed by the daemon.
- Writes local tool metadata outside the project when possible.
- Writes only a small `.remork-local.json` binding marker in the local directory when necessary.
- Does not copy files until `remork sync`, unless the user passes an explicit `--sync`.

The local binding marker must not conflict with target project `.git`.

#### `remork sync`

Synchronizes remote state into the local working copy.

Default behavior:

- Fetch remote manifest.
- Compare manifest with local base and working copy.
- Download clean small-file updates.
- Write `.meta` placeholders for files larger than `128MB`.
- Preserve dirty local files.
- Report conflicts instead of overwriting.
- Update local base state after successful materialization.
- Show progress by phase: manifest, plan, download, write, state.

Confirmation behavior:

- Normal interactive mode asks before overwriting clean local files when the situation could surprise the user.
- `--force` accepts safe overwrites of clean files and materializes requested changes.
- `--quiet` avoids prompts; if a prompt would be required, it fails with an actionable message.

#### `remork status`

Shows the state of the bound workspace.

The default text output should group files by action, not by internal state names:

```text
Workspace: remork-host-a:/tmp/remork-e2e
Local:     /Users/tao/remork/e2e

Clean: 42 files
Local changes: 2 files
Remote updates: 1 file
Conflicts: 0 files
Large placeholders: 3 files

Next:
  remork diff
  remork apply
```

`remork status --json` should include machine-readable counts and file lists.

#### `remork apply`

Writes local changes back to remote after base verification.

Default behavior:

- Builds a changeset from dirty local files.
- Excludes `.meta` placeholder edits from remote file content writes.
- Verifies each target's base hash on the daemon.
- Applies changes atomically.
- Updates local base and working copy state after success.
- Leaves local files intact on conflict.

The command should clearly separate:

- Files that will be created.
- Files that will be updated.
- Files that will be deleted.
- Files that are skipped because they are placeholders or ignored metadata.

`remork apply --dry-run` shows the plan without writing remote files.

#### `remork run`

Runs a non-interactive command in the remote workspace.

Example:

```bash
remork run "pytest -q"
remork run -- python train.py --epochs 1
```

`remork run` is the user-facing name. Internally it maps to the existing `exec` API. The CLI may keep `remork exec` as a hidden or compatibility alias, but README should teach `run`.

Default safe mode:

1. Load current directory binding.
2. Fetch daemon status and remote revision.
3. Check local dirty state.
4. Check whether remote changed since last sync.
5. If clean and stale, run a bounded sync preflight.
6. If dirty or conflicted, refuse to run unless the user chooses `--remote-only`.

`--remote-only` runs against current remote state and visibly warns that local pending edits are ignored.

#### `remork shell`

Opens an interactive remote shell in the bound workspace.

Default behavior matches `run` safe mode. If local dirty changes exist, the command should explain why this can mislead the user and offer explicit alternatives:

```text
Local changes exist and are not on the remote.
Run `remork apply` first, or use `remork shell --remote-only` to open a shell against the remote current state.
```

Shell V1 requirements:

- Starts in the remote workspace root by default.
- Supports terminal resize.
- Supports Ctrl-C.
- Returns the remote exit status when possible.
- Logs shell open/close in workspace operation log.
- Does not log terminal transcript.

Detach and attach are V1.1 shell goals. Product V1 should first make the basic interactive shell stable, resize-aware, interruptible, and well documented.

### Layer 2: Learn Later

These commands are useful after the user understands the daily workflow.

```bash
remork pull <path>
remork diff [path]
remork restore <path>
remork log
remork watch
remork host list
remork workspace list
```

#### `remork pull`

Fetches a specific file or directory using the same sync engine as `remork sync`, but with a narrower target and a different large-file policy.

Examples:

```bash
remork pull checkpoints/model.tar.gz
remork pull data/sample/
remork pull --force checkpoints/model.tar.gz
remork pull --quiet checkpoints/model.tar.gz
```

Behavior:

- Pulling a large file prompts before downloading it in interactive mode.
- Pulling a directory uses the same planner as sync.
- If a local file is dirty, prompt before replacing it; fail in `--quiet`; replace only with `--force`.
- Show transfer progress for full files and large files.

#### `remork diff`

Shows local changes relative to the last synced base.

Default behavior:

- Text files show unified diff.
- Binary files show metadata changes.
- Large placeholders show placeholder metadata changes and explain that editing `.meta` does not modify the remote large file.

#### `remork restore`

Discards local dirty changes and restores files from the local base or remote source.

Examples:

```bash
remork restore src/main.py
remork restore --all
```

This command is intentionally separate from `sync` so that sync does not silently erase local work.

#### `remork log`

Shows recent remote operation log entries for the bound workspace.

Example:

```bash
remork log
remork log --limit 50
remork log --json
```

It reads `<remote-workspace>/.remork/log/operations.jsonl` through the daemon `/operations` endpoint. The log is scoped to the remote workspace, not global to the daemon.

#### `remork watch`

Keeps a foreground process open that listens for remote file events and triggers incremental sync.

V1 can also support `remork sync --watch`, but README should teach one preferred command. The preferred command is `remork watch` because it describes the ongoing behavior better.

Correctness rule:

- Events trigger work.
- Manifest reconciliation proves final state.

#### `remork host`

Manages daemon endpoints.

Examples:

```bash
remork host add lab-a --url http://remork-daemon.example.internal:7731 --token-env REMORK_LAB_A_TOKEN
remork host list
remork host remove lab-a
remork host doctor lab-a
```

Most users should only need `host add` once per machine, often copied from README or internal setup notes.

#### `remork workspace`

Lists and inspects local bindings.

Examples:

```bash
remork workspace list
remork workspace info
remork workspace unbind
```

The main setup command remains `remork init`; `workspace` is for discovery and cleanup.

### Layer 3: Debug And Operations

These commands are for maintainers and Agents diagnosing problems.

```bash
remork doctor
remork debug manifest
remork debug events
remork debug api
remork daemon install
remork daemon upgrade
remork daemon status
```

#### `remork doctor`

Checks local config, current directory binding, daemon reachability, token setup, remote root allowlist, local state readability, remote operation log availability, and large-file threshold.

Output should end with a clear result:

```text
OK: workspace is ready
```

or:

```text
FAILED: daemon reachable, but remote root is not allowed
Fix: start remorkd with --root /data/project-a or update remorkd config.
```

#### `remork debug manifest`

Fetches raw or summarized daemon manifest data for a workspace.

This is not a daily sync command. It exists to diagnose scanner behavior, ignored paths, hash policy, and large-file classification.

#### `remork debug events`

Connects to the event stream and prints normalized events. It should make overflows, reconnects, and resync-required signals visible.

#### `remork debug api`

Runs direct daemon API probes and prints request IDs, status codes, latency, and response summaries.

This command helps separate local planner bugs from daemon transport issues.

#### `remork daemon`

Helps install, upgrade, and inspect `remorkd`.

Product V1 should support simple deployment helpers without assuming remote internet access:

```bash
remork daemon install lab-a --root /data/project-a --addr 0.0.0.0:17731
remork daemon upgrade lab-a
remork daemon status lab-a
```

Implementation may use SSH as a deployment convenience, but daemon runtime transport remains direct HTTP/WebSocket over VPN.

## Configuration Model

### Local Config

Global local config should live under:

```text
~/.remork/config.toml
```

It stores host aliases, daemon URLs, token references, and client identity.

Example:

```toml
client_id = "tao-macbook"

[[hosts]]
name = "remork-host-a"
url = "http://remork-daemon-a.example.internal:17731"
token_env = "REMORK_Z00879328_DOCKER_TOKEN"
no_proxy = true

[[hosts]]
name = "remork-host-b"
url = "http://remork-daemon-b.example.internal:17731"
token_env = "REMORK_Z00879328_DOCKER_2_6_TOKEN"
no_proxy = true
```

Token values should not be written into config by default. Store references to environment variables or OS keychain entries.

### Local Binding

Each local working copy needs a binding to its remote workspace.

Preferred local marker:

```text
<local-workspace>/.remork-local.json
```

Example:

```json
{
  "version": 1,
  "host": "remork-host-a",
  "remote_root": "/tmp/remork-e2e",
  "workspace_id": "ws_...",
  "state_dir": "/Users/tao/.remork/state/ws_..."
}
```

The marker contains no secrets. It lets Remork resolve the workspace from the current directory and keeps most mutable state under `~/.remork/state`.

### Local State

Mutable state should live outside the project:

```text
~/.remork/state/<workspace-id>/
  manifest.json
  base/
  index.sqlite
  transfer-cache/
  locks/
```

V1 can keep JSON state if it remains reliable and tested. Productization should allow migration to SQLite when the number of files or state queries makes JSON unwieldy.

### Remote Config

Remote daemon config should support:

```toml
addr = "0.0.0.0:17731"
roots = ["/data/project-a"]
large_file_threshold = "128MB"
token_file = "/etc/remork/token"
allow_cidrs = ["PRIVATE_CIDR", "VPN_CIDR"]
operation_log_enabled = true
```

The daemon must keep operation logs per workspace:

```text
<workspace-root>/.remork/log/operations.jsonl
```

It should continue skipping `.remork` and `.git` during manifest scans.

## Security Model

Product V1 targets personal or small-team use inside a trusted VPN.

Required V1 baseline:

- Daemon binds only to the configured address.
- Remote roots are allowlisted.
- Requests cannot escape the allowlisted root through `..`, absolute path tricks, encoded traversal, or symlink traversal.
- Optional shared token can be enabled per daemon.
- CLI sends `X-Remork-Client-ID` for operation logs.
- CLI sends an authorization header when a host token is configured.
- Operation logs never store file contents, full shell transcripts, token values, or environment secrets.
- README warns users not to expose `remorkd` to the public internet.

Not included in Product V1:

- Per-user accounts.
- RBAC.
- Multi-tenant control plane.
- Central daemon registry.
- mTLS certificate management.

These are later-stage platform concerns. The V1 shared-token model is enough for a few trusted users on VPN while keeping the product shippable.

## Sync, Pull, And Apply Semantics

### Large Files

Default threshold remains `128MB`.

Files larger than the threshold materialize as:

```text
filename.meta
```

The `.meta` file should preserve the original filename and contain:

- Remote path.
- Size.
- Mtime.
- Revision.
- Hash when available.
- Whether the full file has been pulled.
- Suggested pull command.

Editing `.meta` does not modify the remote large file. `status` and `apply` must make that explicit.

### Prompt Policy

Interactive commands can prompt.

Prompt cases:

- Downloading a large file.
- Replacing a clean local file when the user targeted a pull.
- Deleting a clean local file because remote deleted it.
- Applying a large-file replacement.
- Running shell or run while local state is stale.

Non-interactive flags:

- `--force` confirms destructive or large operations where the command semantics allow it.
- `--quiet` suppresses normal progress and prompts; if a prompt would be required, command fails.
- `--json` returns structured output and should not mix human progress bars into stdout.

### Conflict Policy

Conflicts should be explicit and actionable.

Common conflicts:

- Local dirty file and remote update to same path.
- Local delete and remote update.
- Local create and remote path already exists.
- Apply base hash mismatch.
- Large-file placeholder edited as if it were remote content.

The CLI should avoid vague messages like "sync failed". It should name the path, operation, and next command.

## Remote Execution Semantics

### `run` Safe Mode

Default `remork run` protects users from running remote commands while believing local changes are already remote.

It refuses when:

- Local dirty changes exist.
- Local base is stale and cannot be safely synced.
- Current directory is not bound.
- Daemon root does not match binding.

It may proceed when:

- Workspace is clean and current.
- Workspace is clean and stale, and automatic small-file sync succeeds.
- User passes `--remote-only`.
- User passes `--no-sync-check` for diagnostics.

### Shell Safe Mode

Default `remork shell` uses the same checks as `run`.

`--remote-only` should be visually obvious:

```text
Remote-only shell: local pending changes are ignored.
Workspace: remork-host-a:/tmp/remork-e2e
```

After shell exit, CLI should query the remote revision. If the shell changed the workspace, show:

```text
Remote workspace changed during shell session.
Run `remork sync` to update local files.
```

## Operations And Observability

### Operation Log

Remote operation log is the primary audit trail for Remork-originated requests.

It answers:

- Which client ran `apply`, `run`, `download`, `shell`, or `watch`.
- When it started and ended.
- Whether it succeeded, failed, conflicted, or timed out.
- Which paths were changed by apply.
- Which command was executed by run.

It does not claim to identify every filesystem actor. Changes from manual SSH sessions, cron jobs, training jobs, editors, or other tools are discovered through manifest/watch as external remote changes.

### CLI Output

Every command should have a useful human summary and, where useful, `--json`.

Stdout/stderr policy:

- Human result summaries go to stdout.
- Progress bars go to stderr.
- Machine JSON goes to stdout.
- Errors go to stderr and use non-zero exit codes.

### Exit Codes

Define stable exit code categories:

```text
0   success
1   general error
2   invalid usage or config
3   network or daemon unreachable
4   local dirty state blocked operation
5   conflict
6   permission denied
7   prompt required in quiet mode
8   remote command exited non-zero
9   timeout
```

These numeric values are the documented V1 categories and should remain stable for scripts once implemented.

## Packaging And Deployment

### Release Artifacts

Product V1 release should build:

```text
dist/remork-darwin-arm64
dist/remork-darwin-amd64
dist/remork-linux-arm64
dist/remork-linux-amd64
dist/remorkd-darwin-arm64
dist/remorkd-darwin-amd64
dist/remorkd-linux-arm64
dist/remorkd-linux-amd64
dist/remorkd.example.toml
dist/checksums.txt
dist/README-release.md
```

Remote hosts need only the matching `remorkd` binary and config/token files.

### Offline Remote Install

Remote install must not require:

- Go.
- npm.
- apt.
- brew.
- Internet access.

Supported install path:

1. Build release locally or in CI.
2. Copy `remorkd-<os>-<arch>` to remote.
3. Copy config/token file if enabled.
4. Start daemon with explicit root and bind address.
5. Verify `GET /status` and workspace manifest.

`remork daemon install` may automate this with SSH when available, but SSH is a deployment helper, not the runtime transport.

### Service Management

V1 should support at least documented daemon startup.

On Linux, provide a systemd unit template when systemd exists. Also provide a plain `nohup` or shell script path for containers and minimal hosts.

The two known remote validation hosts are Linux arm64 and should be validated through copied binaries:

```text
remork-host-a
remork-host-b
```

## README Learning Path

The README should be written for humans first.

Recommended structure:

1. What Remork is.
2. Five-minute local-to-remote workflow.
3. The six commands you must know.
4. Daily workflow examples.
5. Large files.
6. Applying local edits safely.
7. Running commands and shell.
8. Operation log.
9. Advanced commands.
10. Debug and maintenance commands.
11. Offline daemon deployment.
12. Safety model and limitations.

The README should avoid opening with API routes or internal package names. API details can move to a developer section after user workflows.

## Acceptance Criteria

### Local E2E

A local test should prove:

1. Start `remorkd` against a temporary remote workspace.
2. Add host config.
3. `remork init` a local directory.
4. `remork sync` materializes small files and large `.meta` placeholders.
5. Edit local file.
6. `remork status` shows dirty state.
7. `remork diff` shows text diff.
8. `remork apply` updates remote.
9. `remork run "cat file"` sees applied remote content.
10. `remork shell --remote-only` can execute a simple command.
11. `remork log` shows relevant operations.

### Conflict E2E

A test should prove:

1. Sync a file.
2. Modify the local file.
3. Modify the remote file before apply.
4. `remork apply` refuses with conflict.
5. Local dirty content remains intact.
6. `remork status --json` reports conflict.

### Large File E2E

A test should prove:

1. Remote has a file larger than `128MB`.
2. `remork sync` creates `filename.meta`.
3. `remork pull filename` prompts in interactive mode.
4. `remork pull --quiet filename` fails if confirmation is required.
5. `remork pull --force filename` downloads full content with progress.
6. `remork status` distinguishes pulled large files from placeholders.

### Remote Host E2E

Both known remote hosts should pass:

1. Build `remorkd-linux-arm64` locally.
2. Copy binary to remote.
3. Start daemon without installing Go or using the internet.
4. Connect from local over VPN HTTP/WebSocket with proxy bypass where needed.
5. Run `init`, `sync`, `status`, `run`, `apply`, `shell`, and `log`.
6. Verify remote operation log lives under `<workspace>/.remork/log/operations.jsonl`.
7. Clean up `/tmp/remorkd*` and `/tmp/remork-e2e`.

### Documentation Acceptance

README is acceptable when a new user can answer:

- What are the six commands I need first?
- How do I bind a local folder?
- How do I avoid overwriting remote or local work?
- How do I pull a large file?
- How do I run a remote command?
- How do I see what Remork did?
- How do I install the daemon on an offline server?
- Which commands are advanced or debug-only?

## Roadmap After Product V1

### V1.1

- Better shell detach and attach UX.
- More complete daemon service management.
- Token storage through macOS Keychain or other OS credential stores.
- Richer `doctor` diagnostics.
- Improved operation log filtering.

### V1.2

- Background local watch service.
- Remote search and indexing.
- Better large-file cache management.
- More efficient state store for very large workspaces.

### V2

- Multi-user identity.
- Workspace ACLs.
- Central daemon registry.
- mTLS.
- Web dashboard if command-line usage shows a real need.

## Open Decisions For Implementation Planning

These are choices to settle in the implementation plan, not gaps in the product design:

- Whether local state remains JSON for V1 or moves to SQLite immediately.
- Whether `remork exec` remains visible or becomes a hidden alias for `remork run`.
- Exact format of `.remork-local.json`.
- Exact token header name.
- Exact systemd unit template.
