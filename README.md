<p align="center">
  <img src="docs/assets/header.png" alt="Remork: remote workspace control for private servers" width="100%">
</p>

# Remork

Remote workspace control for private servers.

[中文文档](README_ZH.md)

Remork gives you a local, editable working copy for a directory that lives on a
remote server. You sync from the remote workspace, edit locally, review the
diff, and explicitly apply changes back. The same daemon can run commands and
interactive shells on the remote machine.

It is built for trusted VPN or private-network environments where installing a
full agent stack on every server is impractical.

## Why Remork

Use Remork when:

- the remote server cannot easily install or update a full local agent runtime;
- humans and agents both need to inspect and edit remote workspace files;
- large model/data artifacts should stay remote unless explicitly pulled;
- writes back to the remote workspace should be reviewed and explicit;
- commands should run on the server, but editing can happen locally.

Remork is not a public multi-tenant remote execution platform. Product V1 has
allowed roots and optional shared-token authentication, but no user accounts,
RBAC, or public-internet hardening.

## What You Get

- `remork sync` pulls remote files into a local working copy.
- `remork diff` and `remork apply` review and write local edits back.
- `remork run -- COMMAND` runs non-interactive commands remotely.
- `remork shell` opens an interactive remote shell through the daemon.
- Large files are represented as `.meta` placeholders until explicitly pulled.
- `remork daemon install` copies a prebuilt daemon over SSH; the server does
  not need Go, npm, apt, brew, or internet access.
- `remork` without a subcommand opens an interactive command menu in a real
  terminal.
- Human-readable commands use inline TUI-style output: colored sections,
  progress bars, tables, warnings, and next-step commands.

## Status

Remork is currently a Product V1 beta. It is usable for small teams and
agent-assisted remote development on trusted private networks.

Release binaries:

```text
remork-darwin-arm64     macOS client, Apple Silicon
remork-darwin-amd64     macOS client, Intel
remorkd-linux-arm64     Linux daemon, arm64
remorkd-linux-amd64     Linux daemon, amd64
```

## Quick Start

This path installs the macOS client, then uses the guided setup flow to prepare
a Linux daemon, bind a local folder, and sync the remote workspace.

### 1. Install the macOS client

```bash
VERSION=v0.1.1.beta02
case "$(uname -m)" in
  arm64) CLIENT_PLATFORM=darwin-arm64 ;;
  x86_64) CLIENT_PLATFORM=darwin-amd64 ;;
  *) echo "unsupported local macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac

mkdir -p "$HOME/.local/bin"
curl -L -o "$HOME/.local/bin/remork" \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-${CLIENT_PLATFORM}"
chmod 0755 "$HOME/.local/bin/remork"
export PATH="$HOME/.local/bin:$PATH"
remork version
```

If a new terminal cannot find `remork`, add this to your shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### 2. Run guided setup

For a human terminal, prefer the product setup flow:

```bash
remork setup
```

The setup flow asks what you want to do: connect this project, prepare a server,
update an existing server, or repair an existing setup. It builds the same
operation specs used by the scriptable commands, shows a review plan, and only
then changes host, daemon, or workspace state.

### 3. Scriptable daemon install

Use the advanced daemon commands when you need a non-interactive script. This is
the same operation path that setup uses internally.

The recommended install uses a shared token. The token protects the daemon from
unauthenticated clients on the private network.

```bash
HOST_ALIAS=my-lab
SSH_TARGET=user@my-server
DAEMON_URL=http://remork-daemon.example.internal:17731
ALLOWED_ROOT=/home/me

export REMORK_TOKEN="$(openssl rand -hex 32)"
mkdir -p "$HOME/.remork"
printf '%s\n' "$REMORK_TOKEN" > "$HOME/.remork/remork.token"
chmod 0600 "$HOME/.remork/remork.token"
REMOTE_TOKEN_FILE=".remork/remork.token"

printf '%s\n' "$REMORK_TOKEN" | ssh "$SSH_TARGET" \
  "mkdir -p \"\$HOME/.remork\" && umask 077 && cat > \"\$HOME/$REMOTE_TOKEN_FILE\""

remork daemon install "$HOST_ALIAS" \
  --ssh "$SSH_TARGET" \
  --url "$DAEMON_URL" \
  --root "$ALLOWED_ROOT" \
  --token-file "$REMOTE_TOKEN_FILE" \
  --token-env REMORK_TOKEN \
  -y \
  --verify \
  --no-proxy
```

Without `--dry-run`, `remork daemon install` and `remork daemon upgrade` show
the plan and ask for confirmation in an interactive terminal. Use `-y/--yes`
for scripts and non-TTY shells. Use `--dry-run` only when you want to preview
the SSH/SCP plan without changing the server. In the interactive daemon form,
Remork shows the deployment parameters on one screen, including roots, SSH
target, daemon URL, auth settings, verification, dry-run, and
`--allow-unauthenticated-network-bind`. If validation fails before remote
mutation, the same form opens again with your previous values.

For future terminals, load the same token before running Remork:

```bash
export REMORK_TOKEN="$(cat "$HOME/.remork/remork.token")"
```

Remork auto-detects the remote Linux platform over SSH. Pass `--platform` only
when auto-detection is unavailable. Repeat `--root` when one daemon should serve
multiple base directories.

### 4. Bind and sync a workspace

```bash
LOCAL_WORKING_COPY=~/remork/project
WORKSPACE_ROOT=/home/me/project

mkdir -p "$LOCAL_WORKING_COPY"
cd "$LOCAL_WORKING_COPY"

remork init "$HOST_ALIAS:$WORKSPACE_ROOT"
remork sync
remork status
```

`remork init` does not install the daemon. It only binds the current local
directory to a remote workspace already served by `remorkd`.

## Daily Workflow

```bash
remork sync

# edit files locally

remork status
remork diff
remork apply
```

Run a command on the remote workspace:

```bash
remork run -- pwd
remork run "make test"
```

Open an interactive remote shell:

```bash
remork shell
```

Use `run` for scripts and agents. Use `shell` for humans who need an
interactive terminal.

To discover commands interactively, run:

```bash
remork
```

The root menu groups daily commands, setup commands, and diagnostic commands so
humans can discover the CLI surface without memorizing every command. It is a
launcher and discovery aid: commands that require a path, host, or shell command
still need explicit input or their command-specific prompt.

## Core Concepts

| Term | Meaning |
| --- | --- |
| Daemon | `remorkd`, the small HTTP service running on the remote server. |
| Remork host | Local nickname for a daemon endpoint, for example `my-lab`. |
| SSH target | SSH destination used only for daemon install or upgrade. |
| Daemon URL | HTTP URL the client uses at runtime. It is not the SSH port. |
| Allowed root | Remote base directory that `remorkd` is allowed to serve. |
| Workspace root | Concrete project directory bound to a local working copy. |
| Local working copy | Local folder you edit. |
| Sync snapshot | Local metadata used to detect local edits and remote conflicts. |

`remorkd --root /home/me` allows workspaces under `/home/me`. A local folder can
then bind to `/home/me/project`, `/home/me/another-project`, or any other child
directory under that allowed root.

## Commands

### Daily commands

| Command | Purpose |
| --- | --- |
| `remork sync` | Pull remote state into the local working copy. |
| `remork status` | Show local edits, remote updates, conflicts, and large-file placeholders. |
| `remork diff` | Review local edits against the last synced base. |
| `remork apply` | Write reviewed local edits back to the remote workspace. |
| `remork run -- COMMAND` | Run a non-interactive command remotely. |
| `remork shell` | Open or attach to an interactive remote shell session. |

### Setup and inspection

```bash
remork host list
remork daemon status my-lab
remork workspace
remork workspace list --json
remork doctor
```

### Automation-friendly output

```bash
remork init HOST:/remote/project --non-interactive
remork sync --json
remork status --json
remork apply --yes --non-interactive
remork doctor --json
remork daemon status HOST --json
remork sync --quiet --non-interactive
```

Interactive and ordinary text output is meant for humans. For agents and
scripts, use `--non-interactive` globally, and use command-specific flags such
as `--json`, `--quiet`, and `--yes` only on commands that support them. `--yes`
is intended for reviewed write/deploy flows such as `apply` and daemon
execution. `--color=never` only disables ANSI color; it does not make human
text output machine-parseable.

Every command also has detailed CLI help:

```bash
remork init -h
remork daemon install -h
remork shell -h
```

## Large Files

Files larger than the daemon threshold are not downloaded by default. Product
V1 uses a `128MB` threshold unless the daemon is started with a different
value.

For a remote file like:

```text
checkpoints/model.tar.gz
```

the local working copy receives:

```text
checkpoints/model.tar.gz.meta
```

Download the full content only when needed:

```bash
remork pull checkpoints/model.tar.gz
```

For scripts or agents, confirm the large download explicitly:

```bash
remork pull --force checkpoints/model.tar.gz
```

## Applying Changes Safely

The remote workspace is the source of truth. Local edits are never pushed
automatically.

`remork apply` sends the base hash captured during `sync` or `pull`. If the
remote file changed after that base was recorded, the daemon rejects the write
instead of overwriting newer remote content.

Broad `remork apply` skips untracked local files by default. To create a new
remote file:

```bash
remork apply path/to/new-file --include-untracked
```

To include all untracked files that are not ignored:

```bash
remork apply --include-untracked
```

`remork apply` is for reviewed source-sized edits. Files larger than `128MB`
are rejected before upload. Keep large artifacts remote and use
`remork pull --force` only when you need a local copy.

Use `.remorkignore` for files that should never be applied, such as caches,
secrets, virtual environments, generated outputs, and agent scratch files.
Remork reads `.remorkignore` before `.gitignore`.

## Remote Commands and Shells

`remork run` executes a command in the bound remote workspace:

```bash
remork run -- pwd
remork run "pytest -q"
remork run --timeout 30s "go test ./..."
```

Before running, Remork checks local and remote workspace state. If local edits
or conflicts make the command unsafe, it stops and tells you what to do next.
Use `--remote-only` only when you intentionally want to ignore local pending
edits.

Command output is currently replayed after the remote command completes. For
long-running interactive work, use `remork shell`; for scripts, use `run` and
set `--timeout` when the command should have a hard limit.

`remork shell` opens an interactive remote shell through the daemon. It is not
plain SSH, but it behaves like a remote interactive shell: it starts in the
workspace root, uses the remote user's interactive shell, and supports attach /
kill for retained sessions.

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

`remork shell` requires a real terminal. Scripts and agents should use
`remork run -- COMMAND`.

## Troubleshooting

### `connect: connection refused`

The client reached the daemon URL host/port, but nothing is listening there.
Check the saved host URL and daemon status:

```bash
remork host list
remork daemon status HOST
```

Install or restart the daemon with `remork daemon install ... -y --verify` in
scripts, or run `remork daemon install ...` in an interactive terminal and
confirm the prompt. Add `--dry-run` to preview without changing the server.

### `remote root is not advertised`

The daemon is alive, but the workspace path is outside its allowed roots.
Restart or reinstall `remorkd` with a `--root` that contains the workspace.

### `token env "REMORK_TOKEN" is not set`

The host entry was configured with `--token-env REMORK_TOKEN`. Load the token
before using Remork:

```bash
export REMORK_TOKEN="$(cat "$HOME/.remork/remork.token")"
```

### New file skipped by `apply`

Untracked files are skipped by default. Apply a specific new file or opt in to
untracked files:

```bash
remork apply path/to/new-file --include-untracked
```

### Only a `.meta` file was synced

The remote file is larger than the large-file threshold. Pull the full file
explicitly:

```bash
remork pull --force path/to/file
```

## Security Model

Remork Product V1 assumes:

- trusted VPN or private-network access;
- explicit daemon allowed roots;
- optional shared-token authentication through token files and environment
  variables;
- no automatic local-to-remote writes;
- no dependency installation on the remote server.

Current limitations:

- no user accounts, RBAC, or multi-tenant isolation;
- no public-internet hardening;
- daemon configuration is primarily flag-based;
- local config is stored under `~/.remork`.

On a trusted VPN or private network you can skip token setup and pass
`--allow-unauthenticated-network-bind` during install. Without a token, Remork
refuses executed non-loopback installs unless that flag is passed explicitly.

## Development

```bash
go test ./...
go vet ./...
scripts/build-release.sh v0.1.1.beta02
```

CI runs tests, vet, and release build checks on pushes and pull requests.

## Documentation

- [中文 README](README_ZH.md)
- [Daemon API](docs/remork-api.md)
- [Agent operating guide](skills/remork/SKILL.md)
- [Product V1 validation notes](docs/remork-product-v1-validation.md)
- [Reliability validation notes](docs/remork-v1-10x-reliability.md)
