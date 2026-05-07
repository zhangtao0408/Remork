# Remork Existing Daemon Connect Design

## Status

Approved design baseline from the May 7, 2026 brainstorming session.

This spec defines the product flow for connecting a local Remork client to an
already running `remorkd` HTTP daemon, plus the matching server-side local TUI
and npm distribution shape. It intentionally does not implement the change.

## Context

Remork already uses HTTP request/response APIs for manifests, downloads, apply,
exec, operations, and status. It also uses WebSocket endpoints for file events
and interactive shell sessions. The current `remork shell` command is not an SSH
shell; it dials the daemon `/shell` WebSocket and the daemon starts a remote PTY.

SSH remains useful for `remork daemon install` and `remork daemon upgrade`,
where the local client copies or updates a daemon binary. That path should
continue to exist. The missing product path is different: when a remote server
already exposes a `remorkd` port, a local user should be able to connect by
entering a daemon URL and an optional token, then immediately get a normal local
working copy, remote command execution, file transfer, and HTTP-backed shell.

## Goals

- Add a first-class client flow for quickly connecting to an existing daemon.
- Let a user enter a daemon URL and optional token without using SSH.
- Save local host and workspace binding state so daily commands continue to use
  the existing `sync`, `status`, `apply`, `run`, and `shell` paths.
- Make token setup recoverable when a previously unauthenticated server later
  enables token authentication, or when a token rotates.
- Add a server-side TUI so a user on the remote machine can configure or modify
  `remorkd` without hand-writing flags.
- Make `remorkd` installable directly on Linux servers through npm.
- Preserve the current SSH-based daemon install and upgrade path.

## Non-Goals

- Do not replace HTTP or WebSocket daemon APIs.
- Do not make `remork shell` use SSH.
- Do not remove `remork daemon install`, `remork daemon upgrade`, `host add`, or
  `init`.
- Do not build full system service integration in this pass.
- Do not store token secrets in workspace binding markers.
- Do not add a temporary `--url` and `--token` mode to every daily command.

## Product Command Model

Add a new client command:

```text
remork connect
remork connect --url http://server:17731
```

Also add the same flow to the human setup menu as:

```text
Connect to existing daemon
```

`remork connect` is the product path for an already running daemon. It configures
the local machine and binds the current directory. It does not install, upgrade,
or start the daemon on the remote server.

The existing advanced primitives remain available:

- `remork daemon install`: install or start a daemon over SSH.
- `remork daemon upgrade`: upgrade a daemon over SSH.
- `remork host add`: manually save a daemon endpoint.
- `remork init`: manually bind the current directory to a configured host.

Daily commands keep resolving their context from the local workspace binding.
After connect succeeds, users should run the same workflow they already use:

```text
remork sync
remork status
remork diff
remork apply
remork run -- COMMAND
remork shell
```

## Client Connect Flow

The default `remork connect` flow is:

1. Collect daemon URL and optional token.
2. Probe `GET /status`.
3. If the probe returns HTTP 401 or 403, prompt for a token and retry.
4. Choose a workspace root from the advertised allowed roots.
5. Verify the workspace root with `GET /manifest`.
6. Write or update host config.
7. Write the current directory workspace binding.
8. Offer first sync, defaulting to yes in interactive mode.

Interactive mode should use the existing TUI form system and preserve entered
values when validation fails, matching the existing setup TUI behavior.

Non-interactive mode should require enough flags to avoid prompts:

```text
remork connect --url http://server:17731 --root /home/me/project --token-file ~/.remork/tokens/lab.token --yes
```

If non-interactive mode hits missing token, wrong token, ambiguous root, or
unsafe overwrite decisions, it should fail with a clear recovery command instead
of prompting.

## Workspace Root Resolution

`GET /status` returns allowed base roots. These are safety boundaries, not
necessarily the exact project directory.

The connect form should expose a `Workspace path` field:

- Empty value: bind the selected allowed root.
- Relative path: join it under the selected allowed root.
- Absolute path: use it directly, but only if it is contained by one advertised
  allowed root.

Examples:

```text
allowed root: /home/me
workspace path: <empty>
resolved workspace root: /home/me

allowed root: /home/me
workspace path: project-a
resolved workspace root: /home/me/project-a

allowed roots: /home/me, /scratch/me
workspace path: /scratch/me/project-a
resolved workspace root: /scratch/me/project-a
```

`~` is not expanded in the client connect path. The client is speaking HTTP, not
opening a remote shell, so users should enter absolute remote paths such as
`/home/me/project`.

When multiple allowed roots exist, the TUI should let users select the default
base root. If the entered workspace path is absolute, the client may ignore that
selection and match the path against all advertised roots. If no advertised root
contains the absolute path, connect fails before writing local state.

## Token Storage

Extend host config to support both token environment variables and token files:

```json
{
  "name": "lab",
  "url": "http://server:17731",
  "token_env": "REMORK_TOKEN",
  "token_file": "/Users/me/.remork/tokens/lab.token",
  "no_proxy": true
}
```

`token_env` remains supported for scripts, CI, and users who prefer shell-managed
secrets. `token_file` is the default for `remork connect` because users can enter
a token once and keep using daily commands from future shells.

The default token file location is:

```text
~/.remork/tokens/<host>.token
```

Token files must be written with `0600` permissions where the platform supports
Unix permissions. `.remork-local.json` must never contain token material.

Token lookup priority should be explicit and stable:

1. If `token_env` is configured and non-empty, use it.
2. Else if `token_file` is configured, read and trim it.
3. Else use no token.

If both `token_env` and `token_file` are configured and `token_env` exists but
is empty, treat it as an auth configuration error and show a fix. This avoids
silently falling through to a file when the user expected the environment to be
authoritative.

## Token Recovery

Authentication must be recoverable.

During `remork connect`, 401 and 403 responses from `/status` or `/manifest`
should reopen the token prompt, write the updated token file, and retry the
current probe.

During daily bound-workspace commands, if the daemon returns 401 or 403:

- If the command is running in an interactive terminal and the host uses
  `token_file`, prompt for an updated token, rewrite the file, and retry the
  failed operation once.
- If the host uses `token_env`, explain that the environment variable must be
  updated and do not rewrite files.
- If the host previously had no token configured, offer to save a token file in
  interactive mode and retry once.
- In non-interactive mode, fail with an exit code for invalid usage or
  permission and print the exact repair command.

This covers the case where a server was originally open, then later restarted
with a token.

## Host Naming

Interactive connect should propose a stable host name derived from the daemon
URL host and port, then let the user edit it.

Examples:

```text
http://10.0.0.5:17731 -> 10-0-0-5-17731
http://lab.example.internal:17731 -> lab-example-internal-17731
```

If the generated name already exists, the form should allow overwrite after a
review step. Overwriting a host must not delete unrelated workspace bindings,
but the review should warn when existing workspaces reference that host.

## First Sync

After writing the binding, connect should offer first sync.

Interactive default:

```text
Run first sync? yes
```

If first sync fails, the host and binding remain written. The result should show
the next command:

```text
remork sync
```

This is important because authentication and root verification have already
succeeded, and a failed initial download can usually be retried without losing
the connect setup.

## Server-Side Command Model

Add server-local commands to `remorkd`:

```text
remorkd setup
remorkd serve --config ~/.remork/remorkd.toml
remorkd start --config ~/.remork/remorkd.toml
remorkd stop --config ~/.remork/remorkd.toml
remorkd status --config ~/.remork/remorkd.toml
```

The current flag style remains valid:

```text
remorkd --root /home/me --addr 0.0.0.0:17731 --token-file ~/.remork/remork.token
```

`remorkd setup` should be a local TUI for configuring or modifying the server.
It should not require a local Remork client, SSH, or a remote control channel.

## Server TUI

The server TUI should collect:

- `Listen address`: default `0.0.0.0:17731`.
- `Allowed roots`: comma-separated or multi-value absolute paths.
- `Token mode`: generate token, paste or update token, or no token.
- `Token file`: default `~/.remork/remork.token`.
- `Large file threshold`: default `128MB`.
- `PID file`: default `~/.remork/run/remorkd.pid`.
- `Log file`: default `~/.remork/log/remorkd.log`.

No-token configuration on a non-loopback or wildcard listen address is risky.
The TUI may allow it only after an explicit confirmation that this is a trusted
private network. The warning should mention that file transfer, apply, exec, and
shell endpoints would be reachable by anyone who can reach the daemon.

The setup form should reopen with prior values after validation errors.

## Server Config File

`remorkd setup` writes a config file, defaulting to:

```text
~/.remork/remorkd.toml
```

Config shape:

```toml
listen_addr = "0.0.0.0:17731"
allowed_roots = ["/home/me", "/scratch/me"]
large_file_threshold = "128MB"
token_file = "$HOME/.remork/remork.token"
pid_file = "$HOME/.remork/run/remorkd.pid"
log_file = "$HOME/.remork/log/remorkd.log"
```

The daemon should support environment variable expansion for config paths where
that matches current documentation, including `$HOME`.

`serve --config` starts in the foreground. `start --config` uses the configured
PID and log files for a lightweight nohup-managed background process. Full
systemd, launchd, and Windows service management are out of scope for this pass.

After setup, the TUI should show:

- Local server command: `remorkd start --config ~/.remork/remorkd.toml`.
- Client command: `remork connect --url http://HOST:PORT`.
- Token file path, when token auth is enabled.

Generated tokens should not be printed repeatedly in normal output. If users
need to transfer the token to a client, use an explicit reveal step or command.

## npm Distribution

Keep the existing client package:

```text
@zhangtao0408/remork
```

It exposes:

```text
remork
```

Add a server package:

```text
@zhangtao0408/remorkd
```

It should support Linux arm64 and Linux amd64, expose `remorkd`, and install the
matching daemon binary through a thin Node wrapper or package layout equivalent
to the existing client wrapper.

Example server install:

```text
npm install -g @zhangtao0408/remorkd@beta
remorkd setup
remorkd start
```

The existing client npm package already includes Linux daemon binaries for
client-driven setup. The server package is for users who are on the server and
want to install or run `remorkd` directly without a client SSH deploy step.

The first implementation should keep scoped package names to match the current
release chain. Unscoped names can be revisited later.

## Components

Client-side implementation should add or extend these boundaries:

- `ConnectSpec`: typed inputs for URL, host name, token source, root selection,
  workspace path, local root, no-proxy, first sync, and confirmation.
- Host config model: add `token_file` without breaking existing `token_env`.
- Auth resolver: read token from env or file through one shared path.
- Auth recovery helper: handle 401/403 prompts and one retry for interactive
  commands.
- Workspace root resolver: normalize empty, relative, and absolute workspace
  path inputs against advertised roots.
- Setup menu: route `Connect to existing daemon` into the same connect core.

Server-side implementation should add:

- `remorkd` config model and TOML reader/writer.
- Server setup TUI.
- `serve --config` foreground runner.
- `start`, `stop`, and `status` for lightweight process management.
- npm server wrapper and package build script.

## Error Handling

Use specific, actionable failures:

- Invalid daemon URL: require `http://` or `https://`.
- Connection refused or timeout: show URL, port, VPN or firewall hint, and
  `remorkd status` hint for server users.
- 401 or 403: prompt for token in interactive mode or print token repair command
  in non-interactive mode.
- No advertised roots: fail because a connect target must expose at least one
  allowed root.
- Workspace path outside roots: show the path and the allowed roots.
- Manifest verification failed: do not write workspace binding.
- Existing host overwrite: show the existing URL and referenced workspaces.
- Unsafe server bind without token: require explicit confirmation.

## Testing

Client tests should cover:

- Connect with an unauthenticated daemon.
- Connect with a correct token.
- Connect with missing token, then prompt and retry.
- Connect to a daemon that changed from no token to token auth.
- `token_env` compatibility.
- `token_file` reading, trimming, missing file, empty file, and update.
- Single allowed root default.
- Multiple allowed root selection.
- Empty workspace path.
- Relative workspace path.
- Absolute workspace path inside any advertised root.
- Absolute workspace path outside all advertised roots.
- Existing host overwrite warning.
- First sync success and first sync failure.

Server tests should cover:

- Config parsing and writing.
- Token generation and `0600` token file permissions.
- `serve --config` producing the same daemon config as explicit flags.
- `start`, `stop`, and `status` process file behavior.
- Unsafe no-token non-loopback validation.
- Existing explicit flag behavior remains compatible.

npm tests should cover:

- Client package still exposes `remork`.
- Server package exposes `remorkd` on Linux arm64 and amd64.
- Unsupported npm platforms fail clearly.
- Package contents include only the intended wrapper, binaries, README, and
  metadata.

End-to-end validation should cover:

1. Start `remorkd` from the server package or local daemon binary.
2. Run `remork connect` from a clean local directory.
3. Verify host config and workspace binding are written.
4. Run `remork sync`.
5. Run `remork run -- pwd`.
6. Run an HTTP/WebSocket-backed `remork shell` smoke.
7. Rotate the daemon token and verify interactive token update recovery.

## Rollout Notes

This work should be implemented in small pieces:

1. Host token file support and shared auth resolution.
2. `remork connect` spec, root resolution, and config writes.
3. Token recovery for connect and daily commands.
4. Setup menu integration.
5. `remorkd` config file support.
6. `remorkd setup`, `serve --config`, `start`, `stop`, and `status`.
7. Server npm package.
8. Documentation and e2e validation.

The implementation plan should stay TDD-first and keep CLI, daemon, npm wrapper,
and e2e tests close to each behavioral change.
