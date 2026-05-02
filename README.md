# Remork

Remote workspace control for private servers.

Remork runs a lightweight daemon on a remote machine and keeps an editable
working copy on your Mac. You sync files from the remote workspace, edit them
locally, review the diff, and write changes back explicitly with `remork apply`.
The same daemon can also run commands and interactive shell sessions in the
remote workspace.

Remork is designed for trusted VPN or private-network environments. Product V1
supports optional shared-token authentication, but it is not an account system
and should not be exposed directly to the public internet.

## Status

Remork is currently Product V1. It is useful for small teams and agent-assisted
remote development where installing a full agent stack on every server is
impractical.

Supported release binaries:

```text
remork-darwin-arm64     macOS client, Apple Silicon
remork-darwin-amd64     macOS client, Intel
remorkd-linux-arm64     Linux daemon, arm64
remorkd-linux-amd64     Linux daemon, amd64
```

## Install

Install the macOS client:

```bash
VERSION=v0.1.1.beta01
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

Install or start the remote daemon through SSH:

```bash
remork daemon install my-lab \
  --ssh my-lab \
  --url http://remork-daemon.example.internal:17731 \
  --root /home/me \
  --platform linux-arm64 \
  --execute --yes \
  --verify \
  --no-proxy
```

The daemon binary is copied to durable paths under the remote user's home
directory. The remote server does not need Go installed and does not need
internet access.

During an executed install, Remork checks whether `remorkd` is already present
on the remote host, reports the existing version when available, copies the new
binary, verifies the copied binary version, and then verifies daemon `/status`
when `--verify` is used.

Use `linux-amd64` instead of `linux-arm64` for x86_64 servers. Repeat `--root`
when one daemon should manage multiple base directories.

## Quickstart

Bind a local directory to a remote workspace:

```bash
mkdir -p ~/remork/project
cd ~/remork/project

remork init my-lab:/home/me/project
remork sync
remork status
```

Edit files locally, then review and apply:

```bash
remork diff
remork apply
```

Run commands in the remote workspace:

```bash
remork run -- pwd
remork run -- make test
remork shell
```

## Concepts

| Term | Meaning |
| --- | --- |
| Remork host | Local nickname for a daemon endpoint, for example `my-lab`. |
| SSH target | SSH destination used only for daemon install or upgrade. |
| Daemon URL | HTTP URL the client uses at runtime. It is not the SSH port. |
| Allowed root | Remote base directory that `remorkd` is allowed to serve. |
| Workspace root | Concrete project directory bound to a local working copy. |
| Local working copy | Local folder you edit. |

`remorkd --root /home/me` allows workspaces under `/home/me`. A local folder can
then bind to `/home/me/project`, `/home/me/another-project`, or any other child
workspace under that allowed root.

## Common Commands

| Command | Purpose |
| --- | --- |
| `remork sync` | Pull remote state into the local working copy. |
| `remork status` | Show local changes, remote updates, conflicts, and large-file placeholders. |
| `remork diff` | Review local changes against the last synced base. |
| `remork apply` | Write reviewed local changes back to the remote workspace. |
| `remork pull PATH` | Download a full file that was left as a large-file placeholder. |
| `remork run -- COMMAND` | Run a non-interactive command remotely. |
| `remork shell` | Open or attach to an interactive remote shell session. |
| `remork doctor` | Check local config, daemon reachability, root coverage, and logs. |

Host and workspace helpers:

```bash
remork host list
remork daemon status my-lab
remork workspace
```

Longer syncs print stage and operation progress unless `--quiet` or `--json` is
used.

## Large Files

Files larger than the daemon threshold are not downloaded by default. Product V1
uses a `128MB` threshold unless the daemon is started with a different value.

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

## Applying Changes

The remote workspace is the source of truth. Local edits are never pushed
automatically.

`remork apply` sends the base hash captured during `sync` or `pull`. If the
remote file changed after that base was recorded, the daemon rejects the write
instead of overwriting newer remote content.

New local files are not created by a broad `remork apply` unless selected
explicitly:

```bash
remork apply path/to/new-file
remork apply --include-untracked
```

Use `.remorkignore` for files that should never be applied, such as local
caches, secrets, virtual environments, generated outputs, and agent scratch
files. Remork reads `.remorkignore` before `.gitignore`.

## Remote Shells

`remork shell` opens an interactive session through the daemon. Sessions are
retained after the local client disconnects.

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
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

## Documentation

- [中文 README](README_ZH.md)
- [Daemon API](docs/remork-api.md)
- [Product V1 validation notes](docs/remork-product-v1-validation.md)
- [Reliability validation notes](docs/remork-v1-10x-reliability.md)
- [Agent operating guide](skills/remork/SKILL.md)
