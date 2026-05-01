# Remork

## What Remork is

Remork lets you work on a remote directory from a local editable working copy.
You run `remorkd` on the remote machine, bind a local directory to a remote
workspace, sync files down, edit locally, and explicitly apply safe changes back.

The remote host only needs one prebuilt `remorkd` binary. It does not need Go,
npm, apt, brew, or internet access.

Remork is built for trusted VPN or private-network use. Product V1 supports an
optional shared bearer token, but it is not an account system and should not be
exposed directly to the public internet.

## Five-minute workflow

Build release binaries locally:

```bash
scripts/build-release.sh dev
```

Copy the daemon to the remote host and start it:

```bash
scp dist/remorkd-linux-arm64 lab-a:/tmp/remorkd
ssh lab-a 'chmod +x /tmp/remorkd'
ssh lab-a 'nohup /tmp/remorkd --root /data/project-a --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
```

Configure the local CLI:

```bash
dist/remork-darwin-arm64 host add lab-a --url http://10.0.0.12:17731
mkdir project-a && cd project-a
../dist/remork-darwin-arm64 init lab-a:/data/project-a
../dist/remork-darwin-arm64 sync
../dist/remork-darwin-arm64 status
```

Edit files locally, review the diff, then apply:

```bash
remork diff
remork apply
```

## The six commands you must know

`remork init HOST:/absolute/remote/root`

Binds the current local directory to a configured daemon host and remote root.
After this, daily commands can infer the workspace from the current directory.

`remork sync`

Copies remote files into the local working copy and writes local base state.
Large files become `.meta` placeholders by default.

`remork status`

Shows whether the local copy is clean, dirty, missing remote data, or blocked by
conflicts.

`remork apply`

Writes local changes back to the remote root. Apply uses base hashes, so a remote
file that changed after your last sync is rejected instead of overwritten.

`remork run -- COMMAND ...`

Runs a non-interactive command in the remote workspace.

`remork shell`

Opens an interactive remote shell through the daemon.

## Daily examples

Add a daemon endpoint:

```bash
remork host add lab-a --url http://10.0.0.12:17731
remork host list
```

If your local shell has a proxy that cannot reach the VPN address, bypass it
for this host:

```bash
remork host add lab-a --url http://10.0.0.12:17731 --no-proxy
```

Use a shared token without writing the secret into config:

```bash
export REMORK_LAB_TOKEN='replace-with-real-token'
remork host add lab-a --url http://10.0.0.12:17731 --token-env REMORK_LAB_TOKEN
```

Bind and sync a workspace:

```bash
mkdir ~/work/project-a
cd ~/work/project-a
remork init lab-a:/data/project-a
remork sync
remork workspace
```

Run a remote check:

```bash
remork run -- make test
```

Apply one edited local working copy:

```bash
remork status
remork diff
remork apply
```

## Large files

Files larger than the daemon threshold are not downloaded by default. Product V1
uses a `128MB` threshold unless the daemon is started differently.

If the remote workspace has:

```text
/data/project-a/checkpoints/model.tar.gz
```

the local working copy receives:

```text
checkpoints/model.tar.gz.meta
```

The meta file records the remote path, size, revision, and pull command. Use
`remork pull checkpoints/model.tar.gz` when you really need the full content.
The recorded pull command may use a fully qualified
`lab-a:/data/project-a/checkpoints/model.tar.gz` reference so it remains usable
even when copied out of the bound directory.

## Applying safely

Remork treats the remote workspace as the source of truth. Local files are
editable, but they are not automatically pushed.

`remork apply` sends a changeset with the base hash that was captured during
sync or pull. If the remote file no longer matches that base hash, the daemon
returns a conflict and does not partially apply the changeset.

This protects against overwriting:

- another user editing the same remote file;
- a remote command changing generated files;
- an agent applying stale local edits.

Review with `remork status` and `remork diff` before applying.

New local files are not created on the remote by a broad `remork apply` unless
you explicitly select them. Use `remork apply path/to/new-file` when you intend
to create one specific remote file, or `remork apply --include-untracked` when
you intend to apply all untracked local files that are not ignored.

Remork reads ignore rules from `.remorkignore` first and `.gitignore` second.
Use `.remorkignore` for local-only caches, secrets, generated outputs, virtual
environments, and agent scratch files that should never be applied.

## Running commands and shell

Use `run` for non-interactive commands:

```bash
remork run -- pwd
remork run -- make test
remork run -- python scripts/check.py
```

Use `shell` when you need an interactive session:

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

Shell sessions are retained by the daemon after a client disconnect. Use
`remork shell --list` to find retained sessions, `--attach` to reconnect, and
`--kill` to stop one. Idle sessions are retained for the daemon's configured
shell retention window and may be reaped after that.

## Operation log

Each remote workspace has its own operation log:

```text
<workspace>/.remork/log/operations.jsonl
```

The daemon skips `.remork` during manifest scans, so the log is not synced into
the local working copy. Use it to inspect daemon-side actions such as manifest,
download, apply, exec, shell, and operations requests.

Read recent entries:

```bash
remork log
```

## Advanced commands

`remork pull PATH`

Fetches a specific file or directory. This is the explicit path for large files.

`remork diff`

Shows local changes against the synced base.

`remork restore PATH`

Discards local edits and restores from the synced base or remote copy.

`remork log`

Shows recent remote operation log entries.

`remork watch`

Keeps a local working copy refreshed from daemon file events.

`remork host`

Manages daemon endpoint aliases.

`remork workspace`

Inspects or removes local workspace bindings.

Common maintenance forms:

```bash
remork host list
remork host remove lab-a
remork workspace
remork workspace remove
```

## Debug and maintenance commands

`remork doctor`

Checks local config, current workspace binding, token setup, daemon reachability,
remote root allowlist, manifest access, and operation log access.

`remork debug manifest`

Fetches daemon manifest data for troubleshooting scanner, ignore, hash, and
large-file behavior.

`remork debug events`

Connects to daemon file events and prints normalized event information.

`remork debug api`

Runs direct daemon API probes and prints concise transport diagnostics.

`remork daemon status HOST`

Loads the configured host, calls `/status`, and prints daemon version, platform,
roots, large-file threshold, watch support, and local auth state.

`remork daemon install HOST --root /remote/root`

Prints exact offline `scp` and `ssh` commands for copying and starting a
prebuilt daemon. Use `--ssh`, `--platform`, `--local-bin`, `--remote-bin`,
`--addr`, and `--token-file` when the defaults are not correct.

`remork daemon upgrade HOST`

Prints exact commands for replacing the remote daemon binary. Start flags are
included when `--root` is provided.

## Offline daemon deployment

Release packaging creates these files:

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

For a Linux arm64 remote:

```bash
scripts/build-release.sh dev
scp dist/remorkd-linux-arm64 lab-a:/tmp/remorkd
ssh lab-a 'chmod +x /tmp/remorkd'
ssh lab-a 'nohup /tmp/remorkd --root /data/project-a --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
curl --noproxy '*' http://10.0.0.12:17731/status
```

Or run the smoke helper:

```bash
scripts/remote-smoke.sh \
  --host lab-a \
  --probe-host 10.0.0.12 \
  --root /tmp/remork-e2e \
  --port 17731 \
  --binary dist/remorkd-linux-arm64
```

Cleanup:

```bash
ssh lab-a 'if [ -f /tmp/remorkd.pid ]; then kill "$(cat /tmp/remorkd.pid)" 2>/dev/null || true; fi; rm -f /tmp/remorkd.pid /tmp/remorkd.log /tmp/remorkd'
```

## Safety model and limitations

Remork Product V1 assumes:

- trusted VPN or private network access;
- optional shared token auth through an environment variable or token file;
- explicit remote roots passed to `remorkd`;
- no automatic writes from local to remote;
- no remote dependency installation during daemon deployment.

Current limitations:

- no accounts, RBAC, or multi-tenant isolation;
- no public-internet hardening;
- daemon config is primarily flag-based in Product V1;
- local config is JSON under `~/.remork`, even though deployment examples may be
  documented as TOML templates.

## Developer API notes

Most users should stay on the CLI. Daemon API details and request shapes live in
`docs/remork-api.md`.

Agent-facing operating guidance lives in `skills/remork/SKILL.md`.

Run the core validation suite:

```bash
go test ./...
scripts/build-release.sh dev
```
