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

## Root semantics

`GET /status` returns `roots`. These are allowed base roots advertised by the
daemon, not necessarily project directories. They define the server-side safety
boundary. A daemon may advertise one or more allowed base roots.

Endpoint query parameters named `root=<workspace-root>` request one concrete
workspace under an allowed root. For example, if `/home/z00879328` is advertised
in `/status.roots`, a request may use:

```text
root=/home/z00879328/11_Wan22_Adapt
```

The daemon must reject workspace roots outside the advertised allowed base roots.
The `path=<path>` parameters are relative to the requested workspace root.

## Endpoints

```text
GET  /status
GET  /manifest?root=<workspace-root>&path=<path>&recursive=true
GET  /download?root=<workspace-root>&path=<path>
POST /apply?root=<workspace-root>
POST /exec
GET  /operations?root=<workspace-root>&limit=<n>
GET  /events?root=<workspace-root>
GET  /shell?root=<workspace-root>[&session=<id>]
GET  /shell/sessions?root=<workspace-root>
DELETE /shell/sessions?root=<workspace-root>&id=<id>
```

## Status

`GET /status`

Returns daemon version, platform, allowed base roots, large-file threshold,
watch support, and auth state.

## Manifest

`GET /manifest?root=<workspace-root>&path=<path>&recursive=true`

Returns normalized file entries for a workspace-relative path. Entries exclude
`.git` and `.remork`. Large files are represented with metadata so the CLI can
write `filename.meta` placeholders instead of downloading the content.

## Download

`GET /download?root=<workspace-root>&path=<path>`

Returns file bytes for a workspace-relative file. The endpoint supports byte
ranges through the standard `Range` header. Product V1 clients enforce a bounded
download body size. CLI sync and pull paths stream the response to disk instead
of buffering full remote files in memory.

## Apply

`POST /apply?root=<workspace-root>`

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

`GET /operations?root=<workspace-root>&limit=<n>`

Reads recent workspace operation log entries from:

```text
<workspace>/.remork/log/operations.jsonl
```

## Events

`GET /events?root=<workspace-root>`

Opens a WebSocket stream of normalized daemon file events. The CLI exposes this
as `remork watch`.

## Shell

`GET /shell?root=<workspace-root>[&session=<id>]`

Opens a WebSocket-backed interactive shell session. Without `session`, the
daemon starts a durable shell in the requested workspace root. With `session`,
the daemon reattaches the WebSocket to an existing retained session for the same
workspace root.

Shell WebSocket frames:

- Binary frames carry PTY output from the daemon to the client.
- Text or binary input that is not structured JSON is written to the PTY.
- Structured JSON frames may carry control messages such as terminal resize.
- The daemon sends a JSON `ShellFrame` with `type: "exit"` and `exit_code` when
  the remote shell exits.

`GET /shell/sessions?root=<workspace-root>`

Lists retained shell sessions for a workspace root. The response is:

```json
{
  "sessions": [
    {
      "id": "sess-...",
      "command": ["sh"],
      "last_active": "2026-05-01T12:00:00Z"
    }
  ]
}
```

`DELETE /shell/sessions?root=<workspace-root>&id=<id>`

Stops and removes a retained shell session. The endpoint returns `204` when the
session was found and closed. The CLI exposes these APIs as `remork shell`,
`remork shell --list`, `remork shell --attach <session-id>`, and
`remork shell --kill <session-id>`.
