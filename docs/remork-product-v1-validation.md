# Remork Product V1 Validation

Date: 2026-05-01

## Local

- `go test ./test/e2e -run TestRemorkProductFullWorkflow -count=1 -v`: PASS
- `go test ./test/e2e -run TestRemorkProduct -count=3 -v`: PASS
- `go test -race ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./internal/shellclient ./test/e2e`: PASS
- `go test ./...`: PASS
- `scripts/build-release.sh dev`: PASS
- Release artifacts checked:
  - `dist/remork-darwin-arm64`
  - `dist/remorkd-linux-arm64`
  - `dist/checksums.txt`

## NoProxy

The local validation host had proxy variables set, so remote CLI validation was
run with intentionally invalid proxy values:

```bash
HTTP_PROXY=http://127.0.0.1:9
HTTPS_PROXY=http://127.0.0.1:9
http_proxy=http://127.0.0.1:9
https_proxy=http://127.0.0.1:9
ALL_PROXY=socks5://127.0.0.1:9
all_proxy=socks5://127.0.0.1:9
```

Each host was configured with `remork host add ... --no-proxy`. `init`, `sync`,
`apply`, `run`, and `log` all passed under that environment.

Additional focused tests:

- `go test ./internal/client -run TestClientNoProxyDisablesProxyFromEnvironment -count=1 -v`: PASS
- `go test ./internal/shellclient -run TestNewDialerNoProxyDisablesProxyFromEnvironment -count=1 -v`: PASS

## Remote Hosts

### z00879328_docker

- SSH alias: `z00879328_docker`
- Probe URL: `http://175.100.2.7:17741`
- Platform reported by daemon: `linux/arm64`
- Copied `dist/remorkd-linux-arm64` to `/tmp/remorkd`.
- Started daemon with:

```bash
/tmp/remorkd --root /tmp/remork-e2e --addr 0.0.0.0:17741
```

- Verified direct VPN HTTP:
  - `curl --noproxy '*' -fsS http://175.100.2.7:17741/status`: PASS
  - `curl --noproxy '*' -fsS --get http://175.100.2.7:17741/manifest --data-urlencode root=/tmp/remork-e2e --data-urlencode path=. --data-urlencode recursive=true`: PASS
- Ran local CLI workflow:
  - `remork host add z7 --url http://175.100.2.7:17741 --no-proxy`: PASS
  - `remork init z7:/tmp/remork-e2e`: PASS
  - `remork sync`: PASS, downloaded `a.txt`
  - local edit `a.txt` to `from-remork`: PASS
  - `remork apply`: PASS
  - `remork run "cat a.txt"`: PASS, printed `from-remork`
  - `remork log --limit 10`: PASS, showed `apply` and `run`
- Verified remote operation log:
  - `/tmp/remork-e2e/.remork/log/operations.jsonl` existed during validation.
  - Log contained `apply` and `exec`.
- Cleanup:
  - Killed `/tmp/remorkd` by pid file.
  - Removed `/tmp/remorkd`, `/tmp/remorkd.pid`, `/tmp/remorkd.log`, and `/tmp/remork-e2e`.
  - Final process/path check returned no `remorkd` process and no matching temp paths.

### z00879328_docker_2.6

- SSH alias: `z00879328_docker_2.6`
- Probe URL: `http://175.100.2.6:17742`
- Platform reported by daemon: `linux/arm64`
- Copied `dist/remorkd-linux-arm64` to `/tmp/remorkd`.
- Started daemon with:

```bash
/tmp/remorkd --root /tmp/remork-e2e --addr 0.0.0.0:17742
```

- Verified direct VPN HTTP:
  - `curl --noproxy '*' -fsS http://175.100.2.6:17742/status`: PASS
  - `curl --noproxy '*' -fsS --get http://175.100.2.6:17742/manifest --data-urlencode root=/tmp/remork-e2e --data-urlencode path=. --data-urlencode recursive=true`: PASS
- Ran local CLI workflow:
  - `remork host add z6 --url http://175.100.2.6:17742 --no-proxy`: PASS
  - `remork init z6:/tmp/remork-e2e`: PASS
  - `remork sync`: PASS, downloaded `a.txt`
  - local edit `a.txt` to `from-remork`: PASS
  - `remork apply`: PASS
  - `remork run "cat a.txt"`: PASS, printed `from-remork`
  - `remork log --limit 10`: PASS, showed `apply` and `run`
- Verified remote operation log:
  - `/tmp/remork-e2e/.remork/log/operations.jsonl` existed during validation.
  - Log contained `apply` and `exec`.
- Cleanup:
  - Killed `/tmp/remorkd` by pid file.
  - Removed `/tmp/remorkd`, `/tmp/remorkd.pid`, `/tmp/remorkd.log`, and `/tmp/remork-e2e`.
  - Final process/path check returned no `remorkd` process and no matching temp paths.

## Notes

- The remote hosts did not need Go, npm, apt, brew, or internet access.
- Validation used copied prebuilt daemon binaries only.
- A previous interrupted validation attempt left `/tmp/remorkd` processes on both hosts; those were cleaned before the final recorded validation run.
