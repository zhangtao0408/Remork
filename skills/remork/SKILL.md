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
2. Run `remork status`.
3. If the directory is not bound, ask for the intended host and workspace root,
   or bind it:

```bash
remork host add HOST --url http://VPN_OR_PRIVATE_IP:17731 --no-proxy
remork init HOST:/absolute/remote/workspace
remork sync
```

Use `--no-proxy` when the remote address is reachable through VPN/private
network and local proxy variables may not reach it.

## Install Flow

Prefer client-driven daemon install. The `--root` value is the allowed root, not
necessarily the project workspace:

```bash
remork daemon install HOST \
  --ssh SSH_TARGET \
  --url http://VPN_OR_PRIVATE_IP:17731 \
  --root /absolute/allowed/root \
  --platform linux-arm64 \
  --execute --yes \
  --verify \
  --no-proxy
```

Repeat `--root` if one daemon should advertise multiple independent allowed base
roots. Every workspace root must be inside one advertised allowed root.

Then bind a local working copy to the workspace root:

```bash
mkdir -p ~/remork/PROJECT
cd ~/remork/PROJECT
remork init HOST:/absolute/allowed/root/project-directory
remork sync
remork status
```

The daemon URL IP is the VPN/private IP or DNS name reachable from the local
machine on port `17731`. Verify with `remork daemon status HOST`. If the URL
uses a non-default port, pass the same port with `--addr 0.0.0.0:PORT` during
`remork daemon install`.

For additional projects on the same host, bind another child workspace under the
same allowed root with a separate local working copy.

## Daily Workflow

Before reading or editing remote-backed files:

```bash
remork sync
remork status
```

After editing locally:

```bash
remork status
remork diff
remork apply
```

After applying, run the remote check through Remork:

```bash
remork run -- COMMAND ...
```

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

Use `--force` only when overwriting the local copy is intentional.

## Remote Commands

Use `run` for non-interactive commands:

```bash
remork run -- pwd
remork run -- make test
remork run -- python scripts/check.py
```

Use `shell` only for workflows that need interactivity:

```bash
remork shell
remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

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
remork daemon status HOST
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
