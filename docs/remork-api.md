# Remork Daemon API

This document is for tool authors and maintainers. Most users should use the
`remork` CLI rather than calling daemon endpoints directly.

## Transport

Remork Product V1 uses HTTP for request/response operations and WebSocket for
events and shell sessions.

Clients may send:

- `X-Remork-Client-ID`: recorded in the remote workspace operation log.
- `Authorization: Bearer <token>`: required when `remorkd` is started with a
  shared token.

When a host is configured with `remork host add --no-proxy`, the CLI bypasses
local proxy environment variables for HTTP and WebSocket daemon calls.

## Endpoints

```text
GET  /status
GET  /manifest?root=<root>&path=<path>&recursive=true
GET  /download?root=<root>&path=<path>
POST /apply?root=<root>
POST /exec
GET  /operations?root=<root>&limit=<n>
GET  /events?root=<root>
GET  /shell?root=<root>
```

## Status

`GET /status`

Returns daemon version, platform, allowlisted roots, large-file threshold,
watch support, and auth state.

## Manifest

`GET /manifest?root=<root>&path=<path>&recursive=true`

Returns normalized file entries for a root-relative path. Entries exclude
`.git` and `.remork`. Large files are represented with metadata so the CLI can
write `filename.meta` placeholders instead of downloading the content.

## Download

`GET /download?root=<root>&path=<path>`

Returns file bytes for a root-relative file. The endpoint supports byte ranges
through the standard `Range` header.

## Apply

`POST /apply?root=<root>`

Writes a changeset to the remote workspace. Each update includes the base hash
captured during sync or pull. If the remote file changed after that base was
captured, the daemon returns a conflict and does not partially apply the
changeset.

The JSON result includes:

- `applied`: true only when every change was written.
- `conflicts`: paths whose base checks failed. Conflicts are detected before
  mutation, so these responses do not change remote files.
- `partial`: paths successfully changed before an unexpected mutation failure.
- `failed_path`: the changeset path that failed during mutation.
- `error`: non-conflict apply error text when no conflict list carries the
  failure reason.

Remork serializes applies with `<workspace>/.remork/lock/apply.lock` and
verifies the full changeset after taking that lock. This prevents concurrent
applies from interleaving and preserves conflict behavior, but Remork does not
provide arbitrary multi-file filesystem transactions. If `partial` is non-empty,
run `remork status` and `remork sync` before retrying.

## Exec

`POST /exec`

Runs a non-interactive command in the remote workspace. The CLI exposes this as
`remork run`.

## Operations

`GET /operations?root=<root>&limit=<n>`

Reads recent workspace operation log entries from:

```text
<workspace>/.remork/log/operations.jsonl
```

## Events

`GET /events?root=<root>`

Opens a WebSocket stream of normalized daemon file events. The CLI exposes this
as `remork watch`.

## Shell

`GET /shell?root=<root>`

Opens a WebSocket-backed interactive shell session. Product V1 shell sessions
are live sessions; detach and reattach are future workflow goals.
