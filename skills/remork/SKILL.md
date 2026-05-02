---
name: remork
description: Use when an agent needs to work on a remote server through a local editable Remork working copy, run commands through remorkd, apply local edits safely, inspect operation logs, or handle large-file placeholders.
---

# Remork

## Overview

Remork lets an agent edit a local working copy while treating the remote
workspace as the source of truth. Sync first, inspect local changes, then use an
explicit `remork apply` to write back to the remote server.

## First Checks

1. Confirm the current directory is the intended local working copy.
2. Run `remork status`.
3. If the directory is not bound, ask for the intended host/root or run:

```bash
remork host add HOST --url http://REMOTE_IP:17731 --no-proxy
remork init HOST:/absolute/remote/root
remork sync
```

Use `--no-proxy` when the remote address is reachable through VPN/private
network and local proxy variables may not reach it.

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

Use `--force` only when overwriting the local copy is intentional:

```bash
remork pull --force path/to/file
```

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

The remote operation log lives under:

```text
<workspace>/.remork/log/operations.jsonl
```

## Offline Daemon Deployment

Prefer the published release assets when they are available:

```bash
VERSION=v0.1.0
curl -L -o remorkd-linux-arm64.tar.gz \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remorkd-${VERSION}-linux-arm64.tar.gz"
mkdir -p remorkd-linux-arm64
tar -xzf remorkd-linux-arm64.tar.gz -C remorkd-linux-arm64
scp remorkd-linux-arm64/remorkd HOST:/tmp/remorkd
ssh HOST 'chmod +x /tmp/remorkd'
ssh HOST 'nohup /tmp/remorkd --root /remote/root --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
```

For local-agent use on macOS, download the matching macOS client package:

```bash
VERSION=v0.1.0
curl -L -o remork-macos.tar.gz \
  "https://github.com/zhangtao0408/Remork/releases/download/${VERSION}/remork-${VERSION}-darwin-arm64.tar.gz"
mkdir -p remork-macos ~/bin
tar -xzf remork-macos.tar.gz -C remork-macos
install -m 0755 remork-macos/remork ~/bin/remork
```

If a release asset is not available yet, build locally and use the generated
`dist/` binaries:

```bash
scripts/build-release.sh v0.1.0
scp dist/remorkd-linux-arm64 HOST:/tmp/remorkd
```

Do not install Go, npm, apt, brew, or internet-dependent dependencies on the
remote host just to run Remork. The remote server only needs the extracted
`remorkd` binary.

On shared VPNs or multi-user networks, start `remorkd` with `--token-file` and
configure the local host with `remork host add --token-env`. Do not expose an
unauthenticated `0.0.0.0:17731` daemon outside a trusted private network.

## Completion Checklist

Before claiming a remote-backed task is complete:

1. `remork status` is understood.
2. Intended local edits were reviewed with `remork diff`.
3. `remork apply` was run if remote changes were required.
4. Required validation was run with `remork run -- ...`.
5. Relevant `remork log` entries exist for apply/run actions.
