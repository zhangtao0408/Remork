# Remork Daemon

Server daemon for Remork remote workspace control.

## Install

```bash
npm install -g @zhangtao0408/remorkd@alpha
remorkd setup
remorkd start
```

`remorkd setup` writes `~/.remork/remorkd.toml` and can generate a shared token.
Use the printed `remork connect --url http://HOST:PORT` command from your client
machine.

Do not expose `remorkd` directly to untrusted networks. Use token auth on shared
VPNs or multi-user networks.
