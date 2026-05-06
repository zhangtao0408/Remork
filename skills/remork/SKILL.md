---
name: remork
description: Use when an agent needs to work on a remote server through a local editable Remork working copy, run commands through remorkd, apply local edits safely, inspect operation logs, or handle large-file placeholders.
---

# Remork

## Overview

Remork lets an agent edit a local working copy while treating the remote
workspace as the source of truth. Sync first, inspect local changes, then use an
explicit `remork apply` to write back to the remote server.

## Terms

- Remork host: local nickname for a daemon URL.
- SSH target: SSH destination used only to install or upgrade `remorkd`.
- Daemon URL: HTTP endpoint used after install; this is not SSH.
- Allowed root: server-side boundary advertised by `/status.roots`.
- Workspace root: actual remote project directory under an allowed root.
- Local working copy: local directory the agent edits.

## First Checks

1. Confirm the current directory is the intended local working copy.
2. Run `remork status --json` when machine parsing matters; otherwise `remork status`.
3. If the directory is not bound, ask for the intended host and workspace root,
   or bind it:

```bash
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --no-proxy
remork init HOST:/absolute/remote/workspace --non-interactive
remork sync --non-interactive
```

Use `--no-proxy` when the remote address is reachable through VPN/private
network and local proxy variables may not reach it.

## Install Flow

For human terminal sessions, prefer the product setup flow:

```bash
remork setup
```

Setup uses the same operation specs as the advanced commands, shows a review
plan, and only then changes host, daemon, or workspace state.

For agent and script workflows, call the explicit advanced command. The `--root`
value is the allowed root, not necessarily the project workspace:

```bash
export REMORK_TOKEN="$(openssl rand -hex 32)"
mkdir -p "$HOME/.remork"
printf '%s\n' "$REMORK_TOKEN" > "$HOME/.remork/remork.token"
chmod 0600 "$HOME/.remork/remork.token"
REMOTE_TOKEN_FILE=".remork/remork.token"

printf '%s\n' "$REMORK_TOKEN" | ssh SSH_TARGET \
  "mkdir -p \"\$HOME/.remork\" && umask 077 && cat > \"\$HOME/$REMOTE_TOKEN_FILE\""

remork daemon install HOST \
  --ssh SSH_TARGET \
  --url http://VPN_OR_PRIVATE_IP:17731 \
  --root /absolute/allowed/root \
  --token-file "$REMOTE_TOKEN_FILE" \
  --token-env REMORK_TOKEN \
  -y --non-interactive \
  --verify \
  --no-proxy
```

Without `--dry-run`, daemon install and upgrade show the plan and execute only
after confirmation. Agent and script flows should pass `-y/--yes
--non-interactive` for explicit, stable remote mutation. Use `--dry-run` only
when the task is to preview the SSH/SCP plan without changing the server.
In a human interactive terminal, the daemon form exposes all deployment options
on one screen, including `--allow-unauthenticated-network-bind`; if validation
fails before remote mutation, Remork reopens the form with the previous values.

Before later `remork` calls in a new shell or agent session, restore the env var
from the local token file:

```bash
export REMORK_TOKEN="$(cat "$HOME/.remork/remork.token")"
```

Repeat `--root` if one daemon should advertise multiple independent allowed base
roots. Every workspace root must be inside one advertised allowed root.

An executed install reports whether a remote `remorkd` binary already exists,
shows its version when available, verifies the copied binary version, and then
checks daemon `/status` when `--verify` is used. Treat version mismatch,
connection refused, timeout, auth failure, or missing advertised roots as setup
blockers, not as normal warnings.

Only use `--allow-unauthenticated-network-bind` for trusted VPN/private
networks where the user explicitly accepts unauthenticated access. Otherwise use
the token-first flow above; Remork requires explicit approval before executing
an unauthenticated non-loopback daemon bind.

Then bind a local working copy to the workspace root:

```bash
mkdir -p ~/remork/PROJECT
cd ~/remork/PROJECT
remork init HOST:/absolute/allowed/root/project-directory --non-interactive
remork sync --non-interactive
remork status
```

The daemon URL IP is the VPN/private IP or DNS name reachable from the local
machine on port `17731`. Verify with `remork daemon status HOST`. If the URL
uses a non-default port, pass the same port with `--addr 0.0.0.0:PORT` during
`remork daemon install`.

Remork auto-detects the remote Linux server platform over SSH. Pass
`--platform linux-arm64` or `--platform linux-amd64` only when SSH platform
detection cannot be used.

For additional projects on the same host, bind another child workspace under the
same allowed root with a separate local working copy.

## Daily Workflow

Before reading or editing remote-backed files:

```bash
remork sync --non-interactive
remork status --json
```

`remork sync` prints stage and operation progress unless `--quiet` or `--json`
is used. If it is slow, use those progress lines to distinguish manifest fetch,
local scan, file transfer, and state save delays.

Remork defaults to interactive, human-oriented output in a terminal. Agents
should pass the global `--non-interactive` flag for scripted flows, use
command-specific `--json` flags for parsing when available, and use
command-specific `--yes` only when the planned remote write or deploy operation
has already been reviewed.
Do not rely on the root `remork` command menu in agent workflows; call the
specific command directly.

After editing locally:

```bash
remork status
remork diff
remork apply --yes --non-interactive
```

After applying, run the remote check through Remork:

```bash
remork run -- COMMAND ...
```

`remork run` currently replays stdout/stderr after the remote command completes.
For live interactive work, use `remork shell`; for automation, keep using
`remork run -- ...` and set `--timeout` when the command needs a hard limit.

## Safety Rules

- Do not write to remote files by SSH behind Remork's back unless the user asks.
- Do not run `remork apply` while `remork status` reports conflicts.
- Do not ignore local dirty state before `remork run`; Remork blocks unsafe runs
  unless `--remote-only` or `--no-sync-check` is chosen deliberately.
- Do not edit `.remork-local.json`, `.remork/`, or `.git` as part of normal
  workspace changes.
- Treat `filename.meta` as a placeholder for a large remote file, not as the
  real file content.

## Large Files

Remork writes large remote files as `filename.meta` placeholders. Pull the real
file only when the task requires it:

```bash
remork pull path/to/file
```

Agents and scripts should confirm large downloads explicitly:

```bash
remork pull --force path/to/file
```

Use `--force` only when overwriting the local copy or downloading a large remote
file is intentional.

Do not use `apply` for large local artifacts. Files above `128MB` are rejected
before upload; keep them remote and pull them explicitly when needed. If a
tracked file was replaced by a directory, rename or restore one side before
applying.

## Remote Commands

Use `run` for non-interactive commands:

```bash
remork run -- pwd
remork run -- make test
remork run -- python scripts/check.py
```

Use `shell` only from a real terminal for workflows that need interactivity:

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

For scripted or agent work, use `remork run -- ...` instead.

Product V1 shell sessions are durable daemon sessions. If the local client
disconnects, list and reattach to the retained session or kill it explicitly.

## Logs And Debugging

Read recent workspace actions:

```bash
remork log --limit 20
```

Check setup and daemon reachability:

```bash
remork doctor
remork doctor --json
remork daemon status HOST
remork daemon status HOST --json
```

Inspect daemon data when sync behavior is unclear:

```bash
remork debug manifest
remork debug events
remork debug api
```

Manual daemon binary copy is an advanced fallback only. Prefer
`remork daemon install` so the daemon goes to durable remote paths and the local
host config can be verified.

## Completion Checklist

Before claiming a remote-backed task is complete:

1. `remork status` is understood.
2. Intended local edits were reviewed with `remork diff`.
3. `remork apply` was run if remote changes were required.
4. Required validation was run with `remork run -- ...`.
5. Relevant `remork log` entries exist for apply/run actions.
