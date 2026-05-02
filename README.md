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

Download the macOS client and Linux server daemon from the GitHub release page:

```bash
VERSION=v0.1.0
curl -L -o remork \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-darwin-arm64"
chmod 0755 remork
mkdir -p ~/bin
mv remork ~/bin/remork

curl -L -o remorkd \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remorkd-linux-arm64"
chmod 0755 remorkd
```

Copy the daemon to the remote host and start it:

```bash
scp remorkd lab-a:/tmp/remorkd
ssh lab-a 'chmod 0755 /tmp/remorkd'
ssh lab-a 'nohup /tmp/remorkd --root /data/project-a --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
```

`--addr 0.0.0.0:17731` exposes Remork to machines that can reach the
VPN/private address. On shared VPNs or multi-user networks, start `remorkd`
with `--token-file` and configure the CLI with `remork host add --token-env`.

Configure the local CLI:

```bash
remork host add lab-a --url http://10.0.0.12:17731
mkdir project-a && cd project-a
remork init lab-a:/data/project-a
remork sync
remork status
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

### Conflict recovery

When local edits and remote updates touch the same file, start with the verbose
status view:

```sh
remork status --verbose
```

For each conflict, inspect the guided recovery steps:

```sh
remork conflict -- path/to/file
```

Review your local edit against the synced base:

```sh
remork diff -- path/to/file
```

To discard your local edit for that file, restore it back to the synced base
cache. This does not accept the current remote update that caused the conflict:

```sh
remork restore -- path/to/file
```

After restore, check status again:

```sh
remork status
```

If remote updates remain, pull them into the local workspace:

```sh
remork sync
```

Then continue reviewing changes or apply when appropriate:

```sh
remork apply
```

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

`remork restore -- PATH`

Discards local edits and restores from the synced base cache, not the current
remote update. Run `remork status` afterward; if remote updates remain, run
`remork sync`, then continue or apply as appropriate.

`remork conflict -- PATH`

Shows the local diff, restore, status, and apply commands for a conflicted path.

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
`--addr`, and `--token-file` when the defaults are not correct. Add
`--execute --yes` to run the generated commands.

`remork daemon upgrade HOST`

Prints exact commands for replacing the remote daemon binary. Start flags are
included when `--root` is provided. Add `--execute --yes` to run the generated
commands.

## Release downloads and offline daemon deployment

GitHub releases publish plain binaries only:

```text
remork-darwin-arm64     # macOS client, Apple Silicon
remork-darwin-amd64     # macOS client, Intel
remorkd-linux-arm64     # Linux server daemon, arm64
remorkd-linux-amd64     # Linux server daemon, amd64
```

Pick the macOS client binary for your local machine and the Linux daemon binary
for the remote server. The remote server does not need Go or internet access.
The GitHub Release body contains the install commands and checksum values for
that release.

If you need to build a release locally, run:

```bash
scripts/build-release.sh v0.1.0
```

The local build also leaves raw cross-compiled binaries in `dist/` for testing
and manual deployment:

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
dist/RELEASE_BODY.md
```

For a Linux arm64 remote:

```bash
scripts/build-release.sh v0.1.0
remork daemon install lab-a --root /data/project-a --ssh lab-a --platform linux-arm64
```

Daemon deployment is print-only by default. To have Remork run the generated
`scp` and `ssh` commands from this machine:

```bash
remork daemon install lab-a --root /data/project-a --ssh lab-a --platform linux-arm64 --execute --yes
```

The SSH step is only an install helper for copying and starting `remorkd`.
Normal Remork runtime transport is still HTTP to the configured `remorkd`
address.

The generated commands are equivalent to:

```bash
scp dist/remorkd-linux-arm64 lab-a:/tmp/remorkd
ssh lab-a 'chmod 0755 /tmp/remorkd'
ssh lab-a 'nohup /tmp/remorkd --root /data/project-a --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
curl --noproxy '*' http://10.0.0.12:17731/status
```

`--addr 0.0.0.0:17731` exposes Remork to machines that can reach the
VPN/private address. On shared VPNs or multi-user networks, start `remorkd`
with `--token-file` and configure the CLI with `remork host add --token-env`.

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
