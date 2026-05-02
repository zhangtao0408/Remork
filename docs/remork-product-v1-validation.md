# Remork Product V1 Validation

Date: 2026-05-02

## Local Verification

- `go test -count=1 ./internal/cli`: PASS
- `go test -count=1 ./...`: PASS.
- `git diff --check`: PASS.
- `scripts/build-release.sh v0.1.0`: PASS.
- `(cd dist && shasum -a 256 -c checksums.txt)`: PASS.
- `scripts/build-release.sh v0.1.0` produced only:
  - `remork-darwin-arm64`
  - `remork-darwin-amd64`
  - `remorkd-linux-arm64`
  - `remorkd-linux-amd64`
  - `checksums.txt`
  - `RELEASE_BODY.md`

## Product-Hardening Fix Verified During Remote Smoke

An early `remork daemon install --execute --verify` run exposed a real startup bug:
the generated remote shell command could background the stop/start compound
command instead of only `remorkd`. That wrote a shell pid and could leave SSH
waiting on the remote command.

The fix changed the generated start command to run stop and start as sequential
statements, then background only `remorkd`. A regression test now asserts the
generated command uses that sequential form and does not contain the previous
`&& nohup` compound-command shape.

Additional path-safety validation after sub-agent review:

- `TestExecEndpointRejectsSymlinkCwdEscape`: PASS. `/exec` now resolves `cwd`
  with symlink-aware containment checks before running commands.
- `TestDownloadSymlinkEscapeReturnsBadRequest`: PASS.
- `TestDownloadSymlinkParentEscapeReturnsBadRequest`: PASS. `/download` now
  opens files with a descriptor walk that uses `openat` and `O_NOFOLLOW` for
  path components on Linux and macOS.

## Remote Host: z00879328_docker

- SSH alias: `z00879328_docker`
- Daemon URL: `http://175.100.2.7:18141`
- Remote allowed roots:
  - `/root/remork-v1-final-1777709876-50860-a`
  - `/root/remork-v1-final-1777709876-50860-b`
- Remote workspace root: `/root/remork-v1-final-1777709876-50860-b/workspace`
- Local working copy: `/var/folders/v8/47s1dzjn6rj71pbpmwh5qnjc0000gn/T/tmp.MPmy5JCXps/wc`
- Daemon platform: `linux/arm64`
- Large-file threshold: `134217728 bytes`

Validated commands:

- `remork daemon install ... --ssh z00879328_docker --url http://175.100.2.7:18141 --root /root/remork-v1-final-1777709876-50860-a --root /root/remork-v1-final-1777709876-50860-b --platform linux-arm64 --local-bin dist/remorkd-linux-arm64 --addr 0.0.0.0:18141 --execute --yes --verify --no-proxy`: PASS
- `remork daemon status <host-alias>`: PASS, advertised both allowed roots.
- `remork init <host-alias>:/root/remork-v1-final-1777709876-50860-b/workspace`: PASS.
- `remork sync --quiet`: PASS, downloaded `a.txt` and `sub/n.txt`.
- `remork status`: PASS, clean workspace.
- `remork run -- pwd`: PASS, printed the remote workspace root.
- Local edit followed by `remork apply`: PASS, updated remote `a.txt`.
- `remork log --limit 5`: PASS.
- Remote operation log under `workspace/.remork/log/operations.jsonl`: PASS.

Cleanup:

- Killed the pid recorded in `$HOME/.remork/run/remorkd.pid`.
- Removed the temporary remote allowed roots.
- Removed the temporary local Remork home and working copy.

## Remote Host: z00879328_docker_2.6

- SSH alias: `z00879328_docker_2.6`
- Daemon URL: `http://175.100.2.6:18142`
- Remote allowed root: `/root/remork-v1-final-1777709902-51340`
- Remote workspace root: `/root/remork-v1-final-1777709902-51340/workspace`
- Local working copy: `/var/folders/v8/47s1dzjn6rj71pbpmwh5qnjc0000gn/T/tmp.lGMJxX2Yjj/wc`
- Daemon platform: `linux/arm64`
- Large-file threshold: `134217728 bytes`

Validated commands:

- `remork daemon install ... --ssh z00879328_docker_2.6 --url http://175.100.2.6:18142 --root /root/remork-v1-final-1777709902-51340 --platform linux-arm64 --local-bin dist/remorkd-linux-arm64 --addr 0.0.0.0:18142 --execute --yes --verify --no-proxy`: PASS
- `remork daemon status <host-alias>`: PASS, advertised `/root/remork-v1-final-1777709902-51340`.
- `remork init <host-alias>:/root/remork-v1-final-1777709902-51340/workspace`: PASS.
- `remork sync --quiet`: PASS, downloaded `a.txt` and `sub/n.txt`.
- `remork status`: PASS, clean workspace.
- `remork run -- pwd`: PASS, printed the remote workspace root.
- Local edit followed by `remork apply`: PASS, updated remote `a.txt`.
- `remork log --limit 5`: PASS.
- Remote operation log under `workspace/.remork/log/operations.jsonl`: PASS.

Cleanup:

- Killed the pid recorded in `$HOME/.remork/run/remorkd.pid`.
- Removed the temporary remote allowed root.
- Removed the temporary local Remork home and working copy.

## Notes

- The remote hosts did not need Go, npm, apt, brew, or internet access.
- Validation used copied prebuilt daemon binaries only.
- Client-driven install used durable remote paths:
  - `$HOME/.local/bin/remorkd`
  - `$HOME/.remork/run/remorkd.pid`
  - `$HOME/.remork/log/remorkd.log`
- The daemon `--root` is now treated as an allowed base root. The validated
  workspace roots were child directories under that allowed root.
- `remorkd` and `remork daemon install` support repeated `--root` flags for
  multiple allowed base roots; focused CLI tests cover repeated root start
  command generation and verification of all advertised roots.
