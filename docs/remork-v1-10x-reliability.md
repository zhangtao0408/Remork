# Remork V1 10x Reliability Validation

Date: 2026-05-01

## Method

The Master Agent ran ten sequential sub-agent validations. Each sub-agent used
only the public README and CLI behavior, then attempted an end-to-end human
workflow against an isolated local daemon workspace. The runs focused on speed,
repeatability, and user-visible failure modes.

Common workflow:

1. Start `remorkd` with a temporary remote root.
2. Configure an isolated local home and host alias.
3. Bind a local working copy with `remork init`.
4. Run `sync`, inspect large-file placeholders, pull content, edit locally,
   inspect status/diff, apply changes, run a remote command, inspect logs, and
   clean up.

Several rounds added focused edge cases: invalid proxy variables plus
`--no-proxy`, token auth, local/remote conflict, dirty-run blocking, workspace
metadata inspection, and watch/events behavior.

## Results

| Run | Time | Result | Focus |
| --- | ---: | --- | --- |
| 1 | 0.712s | PASS | Baseline E2E, large placeholder, pull, apply, run, log, doctor |
| 2 | 0.499s | PASS | Baseline E2E repeat, pull command inspection |
| 3 | 0.536s | PASS | Baseline E2E repeat, command discoverability notes |
| 4 | 0.574s | PASS | Baseline E2E repeat, log/client metadata notes |
| 5 | 0.924s | PASS | Config isolation, `.remork-local.json`, remote operation log |
| 6 | 0.770s | PASS | Dirty local state blocks `run` before apply |
| 7 | 1.000s | PASS | Remote/local conflict detection and recovery path |
| 8 | 0.829s | PASS | Optional bearer token auth and secret handling |
| 9 | 4.831s | PASS core, issue found | Watch/events nested update reliability |
| 10 | 0.583s | PASS | Remote-only run and placeholder workspace command check |

Core E2E pass rate: 10/10.

Median command-workflow time: 0.741s.

Average command-workflow time including the watch-focused round: 1.126s.

Average command-workflow time excluding the watch-focused round: 0.714s.

## Issues Fixed From The 10 Runs

### Recursive watch/event delivery

Run 9 showed that `remork watch` did not reliably report changes under nested
directories such as `src/main.txt`.

Fix:

- The daemon watcher now recursively registers directories.
- Newly created directories are added while the watcher is running.
- `.git` and `.remork` remain ignored.
- `remork watch` now uses the same sync engine as `remork sync`: it performs an
  initial sync after websocket subscription, then syncs the changed path for
  normal events and performs a full reconcile for delete, rename, and overflow
  events.
- CLI `watch` prints `watching ...` only after the websocket subscription is
  established, so humans and scripts can treat that line as a readiness signal.
- CLI watch now closes promptly on context cancellation.
- The daemon events websocket now notices client disconnects even when no new
  file event is being written, so operation-log finalization and cleanup are
  deterministic.

Regression coverage:

- `internal/watch`: nested file update emits an event.
- `internal/daemon`: `/events` streams nested workspace changes.
- `test/e2e`: `remork watch` streams nested remote events, refreshes the local
  working copy, exits on cancel, and writes the daemon operation log.

### Host and workspace command discoverability

Runs 7 and 10 showed that `host remove`, `host list`, and `workspace` were still
placeholder-shaped surfaces even though README-level users naturally try them.

Fix:

- `remork host` and `remork host list` list configured daemon endpoints.
- `remork host remove NAME` removes a configured endpoint and fails for missing
  names.
- `remork workspace` prints the current local binding.
- `remork workspace remove` removes only the local binding marker.

Regression coverage:

- CLI tests assert host list/remove behavior.
- CLI tests assert workspace inspection and binding removal.

### Large-file pull command documentation

Multiple runs noticed that `.meta` files may include a fully qualified pull
target such as `lab-a:/data/project-a/checkpoints/model.tar.gz`, while the README
mostly showed bound-directory relative usage.

Fix:

- README now documents both forms and explains why the full reference appears in
  the meta file.

## Still Worth Improving After V1

- `host add` and `init` are intentionally quiet on success today; first-time
  users may benefit from concise success output.
- Auth failures are correct but terse; a hint about `--token-env` would improve
  setup speed.
- Conflict recovery is safe but still manual. A guided keep-local/keep-remote
  command would reduce friction.
- Operation logs currently include absolute workspace paths. That is useful for
  debugging, but it should remain documented as local-to-workspace metadata.
- `run` preflight can re-check large materialized files. This is safe, but future
  versions should avoid unnecessary large-file work.
- Pull progress should eventually include clearer size/rate/ETA output for very
  large files.

## Acceptance

This step is accepted when the full test suite, race-focused suite, release
build, and release artifact checks pass after the fixes above.

Verification evidence collected after the final fixes:

```text
$ go test -count=1 ./...
ok  	remork/internal/daemon	5.127s
ok  	remork/internal/watch	6.924s
ok  	remork/internal/workspace	6.962s
ok  	remork/test/e2e	6.899s
... all packages passed or had no test files

$ go test -race -count=1 ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./internal/shellclient ./internal/watch ./test/e2e
ok  	remork/internal/daemon	3.508s
ok  	remork/internal/client	4.581s
ok  	remork/internal/syncer	3.763s
ok  	remork/internal/preflight	3.082s
ok  	remork/internal/shellclient	5.024s
ok  	remork/internal/watch	2.582s
ok  	remork/test/e2e	4.128s

$ go test ./test/e2e -run TestRemorkProductWatchStreamsNestedRemoteEvents -count=20 -v
PASS
ok  	remork/test/e2e	0.785s

$ scripts/build-release.sh dev
building dist/remork-darwin-arm64
building dist/remorkd-linux-arm64
... all darwin/linux amd64/arm64 CLI and daemon binaries built

$ shasum -a 256 -c dist/checksums.txt
remork-darwin-amd64: OK
remork-darwin-arm64: OK
remork-linux-amd64: OK
remork-linux-arm64: OK
remorkd-darwin-amd64: OK
remorkd-darwin-arm64: OK
remorkd-linux-amd64: OK
remorkd-linux-arm64: OK
remorkd.example.toml: OK
README-release.md: OK

$ dist/remork-darwin-arm64 version
remork dev
```

## Remote Smoke After Fixes

After rebuilding release binaries, the Master Agent also ran a real remote smoke
on both provided servers with the new `dist/remorkd-linux-arm64` binary:

| Host | URL | Result | Coverage |
| --- | --- | --- | --- |
| `z00879328_docker` | `http://175.100.2.7:17761` | PASS | copy daemon, start remote root, bad proxy plus `--no-proxy`, `init`, `sync`, large `.meta`, `apply`, `run`, `log`, remote log check |
| `z00879328_docker_2.6` | `http://175.100.2.6:17762` | PASS | copy daemon, start remote root, bad proxy plus `--no-proxy`, `init`, `sync`, large `.meta`, `apply`, `run`, `log`, remote log check |

Both smoke runs used temporary `/tmp/remork-v1-e2e-*` paths and cleaned them
after validation.

Smoke output:

```text
z00879328_docker final remote smoke PASS
z00879328_docker_2.6 final remote smoke PASS
```

Cleanup proof commands returned no output after removing temporary smoke paths:

```bash
ssh z00879328_docker 'rm -rf /tmp/remork-v1-e2e-* /tmp/remork-v1-e2e-final-*; ps -ef | grep remork-v1-e2e | grep -v grep || true; find /tmp -maxdepth 1 \( -name "remork-v1-e2e-*" -o -name "remork-v1-e2e-final-*" \) -print 2>/dev/null | sort'
ssh z00879328_docker_2.6 'rm -rf /tmp/remork-v1-e2e-* /tmp/remork-v1-e2e-final-*; ps -ef | grep remork-v1-e2e | grep -v grep || true; find /tmp -maxdepth 1 \( -name "remork-v1-e2e-*" -o -name "remork-v1-e2e-final-*" \) -print 2>/dev/null | sort'
```

## P0-P2 Hardening Verification

Date: 2026-05-01

After the P0-P2 hardening work, the Master Agent ran the full local and remote
verification pass from the hardening plan.

Verified:

- daemon apply rejects symlink parent and symlink final paths;
- apply reports partial filesystem failures;
- client and daemon have default timeouts and body/output limits;
- untracked local files require explicit selection or `--include-untracked`;
- each local checkout uses an isolated state directory;
- watch has periodic reconcile and burst coverage;
- shell sessions can be listed and reattached;
- non-loopback no-token daemon plans print warnings;
- conflict recovery paths are visible in text output;
- daemon install can execute its generated scp/ssh plan.

Local verification:

```bash
go test -count=1 ./...
go test -race -count=1 ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./internal/shellclient ./internal/watch ./test/e2e
scripts/build-release.sh dev
(cd dist && shasum -a 256 -c checksums.txt)
```

Result: all commands passed. The release check verified all Darwin/Linux
amd64/arm64 CLI and daemon binaries plus `remorkd.example.toml` and
`README-release.md`.

Final remote smoke used the rebuilt `dist/remorkd-linux-arm64` daemon and
`dist/remork-darwin-arm64` CLI against both provided servers. Each run copied a
single daemon binary to the server, started it without requiring remote Go or
internet access, configured the local CLI with a deliberately bad proxy plus
`--no-proxy`, then exercised `host add`, `init`, `sync`, large-file `.meta`
materialization for a 128MiB+ file, `apply`, `run`, `log`, and the remote
workspace operation log under `.remork/log/operations.jsonl`.

| Host | URL | Result | Coverage |
| --- | --- | --- | --- |
| `z00879328_docker` | `http://175.100.2.7:17771` | PASS | copy daemon, detached start, bad proxy plus `--no-proxy`, `init`, `sync`, nested file, 128MiB+ large `.meta`, `apply`, `run`, CLI `log`, remote `.remork/log` check |
| `z00879328_docker_2.6` | `http://175.100.2.6:17772` | PASS | copy daemon, detached start, bad proxy plus `--no-proxy`, `init`, `sync`, nested file, 128MiB+ large `.meta`, `apply`, `run`, CLI `log`, remote `.remork/log` check |

Smoke output:

```text
z00879328_docker final remote smoke PASS
z00879328_docker_2.6 final remote smoke PASS
```

Cleanup proof commands returned no output after removing temporary hardening
smoke paths:

```bash
ssh z00879328_docker 'rm -rf /tmp/remork-v1-hardening-*; ps -ef | grep remork-v1-hardening | grep -v grep || true; find /tmp -maxdepth 1 -name "remork-v1-hardening-*" -print 2>/dev/null | sort'
ssh z00879328_docker_2.6 'rm -rf /tmp/remork-v1-hardening-*; ps -ef | grep remork-v1-hardening | grep -v grep || true; find /tmp -maxdepth 1 -name "remork-v1-hardening-*" -print 2>/dev/null | sort'
```

## Final Review Hardening Follow-up

Date: 2026-05-01

The final code-quality review found additional robustness gaps. They were fixed
and covered with targeted tests:

- sync writes use unique temporary files and do not overwrite user files named
  like `*.remork-tmp`;
- stale `apply.lock` files with dead PIDs are reclaimed instead of wedging all
  future applies;
- shell session retention is enforced when sessions are listed, fetched, or new
  sessions are started;
- CLI sync and pull downloads stream to disk through a bounded client download
  path;
- large-file revisions include nanosecond mtime so same-size updates inside one
  second are visible;
- operation log reads accept large JSONL entries instead of failing at the
  default scanner token limit.
- sync handles remote file-to-directory replacements by deleting clean stale
  local/base-cache files before downloading child files, while dirty local
  replacements become conflicts and block child downloads;
- sync handles the reverse remote directory-to-file replacement by deleting
  clean tracked descendants and empty local/base-cache directories before
  downloading the parent file, while dirty or untracked descendants become
  conflicts;
- operation logging rejects `.remork/log` symlink parents and symlinked
  `operations.jsonl` files so daemon logs cannot be redirected outside the
  workspace root.

Verification after these fixes:

```bash
go test -count=1 ./internal/transfer ./internal/apply ./internal/pty ./internal/client ./internal/manifest ./internal/ops
go test -count=1 ./internal/syncer ./internal/daemon ./test/e2e ./internal/cli ./cmd/remork ./cmd/remorkd
go test -count=1 ./internal/planner ./internal/syncer ./internal/ops ./internal/daemon ./internal/cli ./test/e2e
go test -count=1 ./...
go test -race -count=1 ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./internal/shellclient ./internal/watch ./test/e2e
scripts/build-release.sh dev
(cd dist && shasum -a 256 -c checksums.txt)
```

Result: all commands passed.

The rebuilt `dist/remorkd-linux-arm64` and `dist/remork-darwin-arm64` binaries
were smoke-tested again on both provided hosts:

```text
z00879328_docker final remote smoke PASS
z00879328_docker_2.6 final remote smoke PASS
```

Cleanup proof commands returned no output:

```bash
ssh z00879328_docker 'rm -rf /tmp/remork-v1-hardening-*; ps -ef | grep remork-v1-hardening | grep -v grep || true; find /tmp -maxdepth 1 -name "remork-v1-hardening-*" -print 2>/dev/null | sort'
ssh z00879328_docker_2.6 'rm -rf /tmp/remork-v1-hardening-*; ps -ef | grep remork-v1-hardening | grep -v grep || true; find /tmp -maxdepth 1 -name "remork-v1-hardening-*" -print 2>/dev/null | sort'
```
