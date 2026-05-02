# Remork

Remork lets you edit a remote project from a local working copy. You install a
small daemon on the remote server, bind one local folder to one remote project
directory, sync files down, edit locally, and explicitly apply safe changes back.

Remork is built for trusted VPN or private-network use. Product V1 supports an
optional shared bearer token, but it is not an account system and should not be
exposed directly to the public internet.

## Mental model

- **Remork host**: a local nickname for a daemon endpoint, such as `my-lab`.
- **SSH target**: the SSH name Remork uses only to install or upgrade the daemon.
  This can be the same text as the Remork host, but it is a different concept.
- **Daemon URL**: the HTTP URL the CLI uses after install, such as
  `http://remork-daemon.example.internal:17731`. This is not SSH and does not
  use the SSH port.
- **Allowed root**: the server-side safety boundary advertised by `remorkd`.
  The daemon will only serve workspaces under allowed roots.
- **Workspace root**: the actual remote project directory bound to your local
  working copy.
- **Local working copy**: the local folder you edit.

In the quickstart below, `ALLOWED_ROOT` is the allowed root and
`WORKSPACE_ROOT` is the workspace root.

## Before you start

You need:

- a macOS local machine with `curl` available;
- SSH access from the local machine to the remote server;
- the remote server's VPN/private IP or DNS name reachable from local on port
  `17731`;
- a Linux remote server, usually `linux-arm64` on ARM servers or `linux-amd64`
  on x86_64 servers;
- permission for the remote user to write `$HOME/.local/bin` and
  `$HOME/.remork`;
- an existing remote workspace directory under the allowed root.

Fill these values first:

```bash
HOST_ALIAS=my-lab
SSH_TARGET=my-lab
DAEMON_URL=http://remork-daemon.example.internal:17731
ALLOWED_ROOT=/home/me
WORKSPACE_ROOT=/home/me/project
LOCAL_WORKING_COPY=~/remork/project
REMOTE_PLATFORM=linux-arm64
CLIENT_PLATFORM=darwin-arm64
```

Value guide:

```text
HOST_ALIAS          local Remork nickname
SSH_TARGET          SSH target used only for install or upgrade
DAEMON_URL          HTTP URL reachable from local after install
ALLOWED_ROOT        remote safety boundary served by remorkd
WORKSPACE_ROOT      remote project directory you want to edit
LOCAL_WORKING_COPY  local folder you will edit
REMOTE_PLATFORM     linux-arm64 or linux-amd64
CLIENT_PLATFORM     darwin-arm64 on Apple Silicon, darwin-amd64 on Intel Macs
```

You can set `CLIENT_PLATFORM` automatically:

```bash
case "$(uname -m)" in
  arm64) CLIENT_PLATFORM=darwin-arm64 ;;
  x86_64) CLIENT_PLATFORM=darwin-amd64 ;;
  *) echo "unsupported local macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac
```

Check the remote values before installing:

```bash
ssh "$SSH_TARGET" 'uname -m; pwd'
ssh "$SSH_TARGET" "test -d '$WORKSPACE_ROOT'"
```

## Quickstart

Install the macOS client on your local machine:

```bash
VERSION=v0.1.0
mkdir -p "$HOME/.local/bin"
curl -L -o "$HOME/.local/bin/remork" \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-${CLIENT_PLATFORM}"
chmod 0755 "$HOME/.local/bin/remork"
export PATH="$HOME/.local/bin:$PATH"
```

If a new terminal cannot find `remork`, add this line to `~/.zshrc`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Use the client to install the remote daemon through SSH:

```bash
remork daemon install "$HOST_ALIAS" \
  --ssh "$SSH_TARGET" \
  --url "$DAEMON_URL" \
  --root "$ALLOWED_ROOT" \
  --platform "$REMOTE_PLATFORM" \
  --execute --yes \
  --verify \
  --no-proxy
```

If the same daemon should serve multiple independent base directories, repeat
`--root`, for example `--root /home/me --root /scratch/me`. Every workspace
must live under one of the advertised allowed roots.

This copies the matching `remorkd` binary to durable paths on the remote server
under `$HOME/.local/bin` and `$HOME/.remork`, starts it, writes the local Remork
host config, and verifies the daemon status. On shared VPNs or multi-user
networks, add daemon `--token-file` and configure the local host with
`--token-env` instead of using an unauthenticated daemon.

The daemon URL IP should be the VPN/private IP or DNS name that your local
machine can reach on port `17731`. It is not the SSH port. If you are unsure,
use the server's VPN/private address and check it with:

```bash
remork daemon status "$HOST_ALIAS"
```

If you choose a daemon URL with a different port, pass the same port to install,
for example `--addr 0.0.0.0:18131` with `--url http://HOST:18131`.

Create a local working copy and bind it to the remote workspace root:

```bash
mkdir -p "$LOCAL_WORKING_COPY"
cd "$LOCAL_WORKING_COPY"
remork init "$HOST_ALIAS:$WORKSPACE_ROOT"
remork sync
remork status
remork doctor
```

Edit files locally, review the diff, then apply:

```bash
remork diff
remork apply
```

## Daily commands

`remork sync`

Copies remote files into the local working copy and records the base state used
for conflict checks. Large files become `.meta` placeholders by default.

`remork status`

Shows whether the local copy is clean, dirty, missing remote updates, blocked by
conflicts, or holding large-file placeholders.

`remork diff`

Shows local changes against the synced base.

`remork apply`

Writes local changes back to the remote workspace. Apply uses base hashes, so a
remote file that changed after your last sync is rejected instead of overwritten.

`remork run -- COMMAND ...`

Runs a non-interactive command in the remote workspace.

`remork shell`

Opens an interactive remote shell through the daemon.

## Common workflows

Add or inspect daemon endpoint nicknames:

```bash
remork host list
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --no-proxy
remork host remove HOST
```

Use a shared token without writing the secret into config:

```bash
export REMORK_TOKEN='replace-with-real-token'
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --token-env REMORK_TOKEN
```

Bind another child workspace under the same allowed root:

```bash
mkdir -p ~/remork/MY_PROJECT
cd ~/remork/MY_PROJECT
remork init HOST:/absolute/remote/workspace
remork sync
remork workspace
```

Run a remote check after editing:

```bash
remork status
remork diff
remork apply
remork run -- make test
```

## Large files

Files larger than the daemon threshold are not downloaded by default. Product V1
uses a `128MB` threshold unless the daemon is started differently.

If the remote workspace contains:

```text
checkpoints/model.tar.gz
```

the local working copy receives:

```text
checkpoints/model.tar.gz.meta
```

The meta file records the remote path, size, revision, and pull command. Use
`remork pull checkpoints/model.tar.gz` only when you need the full content.

## Applying safely

Remork treats the remote workspace as the source of truth. Local files are
editable, but they are not automatically pushed.

`remork apply` sends a changeset with the base hash captured during sync or
pull. If the remote file no longer matches that base hash, the daemon returns a
conflict and does not partially apply the changeset.

This protects against:

- another user editing the same remote file;
- a remote command changing generated files;
- an agent applying stale local edits.

Always review with `remork status` and `remork diff` before applying.

### Conflict recovery

Start with the verbose status view:

```bash
remork status --verbose
```

For each conflict, inspect the guided recovery steps:

```bash
remork conflict -- path/to/file
```

To discard your local edit for that file, restore it back to the synced base
cache. This does not accept the current remote update that caused the conflict:

```bash
remork restore -- path/to/file
remork status
remork sync
```

Then continue reviewing changes or apply when appropriate.

New local files are not created on the remote by a broad `remork apply` unless
you explicitly select them. Use `remork apply path/to/new-file` for one new file
or `remork apply --include-untracked` for all untracked local files that are not
ignored.

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
`--kill` to stop one.

## Debug and maintenance

`remork doctor`

Checks local config, current workspace binding, token setup, daemon reachability,
allowed root coverage, manifest access, and operation log access.

`remork daemon status HOST`

Calls the daemon URL configured for `HOST` and prints daemon version, platform,
allowed roots, large-file threshold, watch support, and local auth state.

`remork daemon install HOST --root /allowed/root [--root /another/root]`

Installs or starts `remorkd` through SSH. Use `--ssh` to choose the SSH target,
`--url` to write the local daemon URL, `--platform` to choose the daemon binary,
`--execute --yes` to run the generated install commands, and `--verify` to check
the daemon afterward. Repeat `--root` when one daemon should advertise multiple
allowed base roots.

`remork daemon upgrade HOST`

Replaces the remote daemon binary. Add `--execute --yes` to run the generated
commands.

`remork debug manifest`, `remork debug events`, and `remork debug api`

Inspect daemon data and transport behavior when sync or connectivity is unclear.

## Release downloads and advanced fallback deployment

GitHub releases publish plain binaries:

```text
remork-darwin-arm64     # macOS client, Apple Silicon
remork-darwin-amd64     # macOS client, Intel
remorkd-linux-arm64     # Linux server daemon, arm64
remorkd-linux-amd64     # Linux server daemon, amd64
```

Most users should install the local client to `~/.local/bin/remork` and let
`remork daemon install` deploy the daemon to durable remote paths.

If you need to build a release locally:

```bash
scripts/build-release.sh v0.1.0
```

Advanced fallback only: if client-driven install is unavailable, copy a release
daemon manually and start it yourself. These placeholders are intentionally
generic:

```bash
scp dist/remorkd-linux-arm64 SSH_TARGET:~/.local/bin/remorkd
ssh SSH_TARGET 'chmod 0755 ~/.local/bin/remorkd'
ssh SSH_TARGET 'mkdir -p ~/.remork/run ~/.remork/log'
ssh SSH_TARGET 'nohup ~/.local/bin/remorkd --root /allowed/root --addr 0.0.0.0:17731 </dev/null >~/.remork/log/remorkd.log 2>&1 & echo $! >~/.remork/run/remorkd.pid'
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --no-proxy
remork daemon status HOST
```

Repeat `--root` in the manual `remorkd` command to expose multiple allowed base
roots.

The SSH target is only an install helper for copying and starting `remorkd`.
Normal Remork runtime traffic is HTTP or WebSocket to the daemon URL.

## Safety model and limitations

Remork Product V1 assumes:

- trusted VPN or private network access;
- optional shared token auth through an environment variable or token file;
- explicit allowed roots passed to `remorkd`;
- no automatic writes from local to remote;
- no remote dependency installation during daemon deployment.

Current limitations:

- no accounts, RBAC, or multi-tenant isolation;
- no public-internet hardening;
- daemon config is primarily flag-based in Product V1;
- local config is JSON under `~/.remork`.

## Developer notes

Most users should stay on the CLI. Daemon API details and request shapes live in
`docs/remork-api.md`.

Agent-facing operating guidance lives in `skills/remork/SKILL.md`.
