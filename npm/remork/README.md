# Remork

Remote workspace control for private servers.

## Install

```bash
npm install -g @zhangtao0408/remork@alpha
remork setup
```

This package includes Remork client binaries for macOS and Windows plus Linux
`remorkd` daemon binaries used by `remork setup`.

## Connect to an existing daemon

```bash
remork connect --url http://server:17731
```

Use this when `remorkd` is already running on a reachable server. The client
stores token auth in a local token file when needed.

## Security and Network Safety

Remork is intended for trusted private networks, VPNs, or similarly controlled
server environments. `remork setup` installs or updates a remote HTTP daemon;
do not expose that daemon directly to untrusted networks. When a daemon is
reachable from a shared network, enable token authentication and keep the token
private.

Supported client platforms:

- macOS arm64
- macOS amd64
- Windows arm64
- Windows amd64

Supported server daemon platforms:

- Linux arm64
- Linux amd64

For full documentation, see https://github.com/zhangtao0408/Remork.
