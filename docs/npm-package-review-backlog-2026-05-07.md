# npm Package Review Backlog

Date: 2026-05-07

Branch reviewed: `codex/npm-package`

This document summarizes five serial sub-agent review passes over the npm
package work. The review focused on bugs and follow-up improvements before
publishing the package.

## Resolution Status

Status: fixed and re-reviewed on 2026-05-07.

Fix commits:

- `ddfe561` fixes the Go-side execution ordering and GitHub release tag
  fallback issues.
- `ac42465` hardens the npm package release flow, docs, metadata, CI smoke, and
  publish safeguards.

Final reviewer result: approved. The reviewer found no remaining issues for the
listed backlog items.

Final validation performed:

```bash
go test ./...
go vet ./...
bash scripts/build-release.sh v0.1.1-beta.4
bash scripts/build-npm-package.sh v0.1.1-beta.4
bash scripts/smoke-npm-package.sh
bash scripts/publish-npm.sh npm/remork --dry-run
NPM_TAG=latest bash scripts/publish-npm.sh npm/remork --dry-run
```

The final `NPM_TAG=latest` command is expected to fail for the prerelease; it
was separately verified to reject `0.1.1-beta.4` with the intended refusal
message.

## Review Passes

1. npm wrapper and package runtime behavior.
2. Go daemon binary resolver and setup integration.
3. Release scripts, CI, and package generation.
4. Docs, install UX, publish runbook, and package metadata.
5. Adversarial validation of prior findings and false-positive filtering.

No P0 or P1 issues were found. The items below are the validated backlog.

## P2 Fix Before First npm Publish

### 1. SSH probes can run before confirmation and execution validation

**Area:** setup / daemon install safety

**Files:**

- `internal/cli/commands_daemon.go`

**Problem:**

When `--local-bin` is not provided, `prepareAndRunDaemonDeploy` can run remote
checks before `runDaemonDeploy` enforces confirmation and execution validation.
That means a command that should later fail because it lacks `-y`, has invalid
token settings, or violates unauthenticated network-bind policy can still run
remote SSH probes such as `remorkd --version` or platform detection first.

**Why it matters:**

Setup should not mutate or contact the remote more than necessary before the
user has confirmed the plan and before local security validation has passed.
This is especially important for TUI flows and non-interactive commands.

**Suggested fix:**

- Split deploy preparation into pure local defaulting/plan validation and
  confirmed execution preparation.
- Run local execution validation before any SSH probe.
- Only run remote compatibility/platform probes after confirmation or in a
  clearly labeled preflight phase that has passed local safety checks.
- Add tests proving a missing `-y` or unauthenticated bind rejection does not
  call the fake SSH runner.

### 2. Release fallback may download from the wrong GitHub tag

**Area:** release binary fallback

**Files:**

- `cmd/remork/main.go`
- `internal/cli/release_binary.go`
- `scripts/build-release.sh`

**Problem:**

The CLI now normalizes display/runtime version from `v0.1.1-beta.4` to
`0.1.1-beta.4`. `resolveReleaseDaemonBinary` uses that version directly when it
constructs GitHub Release download URLs. If GitHub tags remain `v`-prefixed,
fallback daemon downloads can request:

```text
/releases/download/0.1.1-beta.4/remorkd-linux-arm64
```

instead of:

```text
/releases/download/v0.1.1-beta.4/remorkd-linux-arm64
```

**Why it matters:**

npm installs should normally use bundled vendor binaries, but direct binary
installs, cache misses, or fallback paths can still hit GitHub Release
downloads. A wrong tag URL would make daemon setup fail in those environments.

**Suggested fix:**

- Keep separate concepts for display version and release tag.
- Add a helper such as `releaseTagForVersion(version string)` that maps
  `0.1.1-beta.4` to `v0.1.1-beta.4`, while keeping `dev` unchanged.
- Add resolver tests that verify the downloader URL uses the `v`-prefixed tag
  for semver release versions.

### 3. npm README lacks the daemon security warning

**Area:** registry-facing documentation

**Files:**

- `npm/remork/README.md`
- `scripts/build-npm-package.sh`

**Problem:**

The npm README tells users to run `remork setup`, but it does not explain that
setup installs a remote HTTP daemon and that unauthenticated non-loopback daemon
binds should only be used on trusted private networks.

**Why it matters:**

The npm README is the primary page users will see on npmjs.com. It should carry
the same safety context as the repository README, especially before first-time
users expose a daemon on a private IP or VPN.

**Suggested fix:**

- Add a short "Security" or "Network safety" section to the generated npm
  README.
- Mention trusted private networks, token auth for shared networks, and
  `remork doctor` / `remork daemon status HOST` as follow-up checks.
- Update `scripts/build-npm-package.sh` so generated README content includes
  this warning.

## P3 Improve Before or Soon After First npm Publish

### 4. Prerelease npm publish needs an explicit dist-tag

**Area:** publish runbook

**Files:**

- `docs/superpowers/plans/2026-05-06-npm-package-implementation.md`
- future publish script or release docs

**Problem:**

Publishing `0.1.1-beta.4` requires an explicit npm dist-tag. A plain
`npm publish` can either fail for a prerelease or risk assigning `latest`,
depending on npm CLI behavior and configuration. A review pass verified that
`npm publish --dry-run --json` fails without a tag and passes with
`--tag beta`.

**Suggested fix:**

- Document first beta publish as:

```bash
npm publish --tag beta
```

- For stable releases, use the default `latest` only intentionally.
- Consider adding a small `scripts/publish-npm.sh` later to encode this rule.

### 5. CI does not run install-level npm package smoke

**Area:** CI coverage

**Files:**

- `.github/workflows/ci.yml`
- `scripts/smoke-npm-package.sh`

**Problem:**

CI builds the npm package and runs wrapper unit tests, but it does not install
the produced tarball and run the installed `remork` command. The smoke script
does this locally on macOS, but CI does not call it.

**Suggested fix:**

- Add `bash scripts/smoke-npm-package.sh` to CI after package build.
- Longer term, add a small OS matrix for at least macOS and Windows so npm
  shims and PowerShell/CMD launch behavior are verified.

### 6. npm package metadata does not block unsupported client platforms

**Area:** npm metadata / unsupported platform UX

**Files:**

- `npm/remork/package.json`
- `scripts/build-npm-package.sh`
- `npm/remork/bin/remork.js`
- `npm/remork/test/remork-wrapper.test.js`

**Problem:**

The package declares only a Node engine. Linux users can install it even though
the wrapper currently supports only macOS and Windows clients. They will fail at
runtime with `unsupported Remork client platform`.

**Suggested fix options:**

- If Linux client support is intentionally absent, add npm metadata:

```json
"os": ["darwin", "win32"],
"cpu": ["arm64", "x64"]
```

- If Linux client support is expected soon, keep install open but make README
  support status explicit.
- Expand wrapper tests to cover all supported combinations:
  `darwin-arm64`, `darwin-x64`, `win32-arm64`, and `win32-x64`.

### 7. Windows npm shim behavior is not verified

**Area:** cross-platform package validation

**Files:**

- `npm/remork/bin/remork.js`
- `npm/remork/test/remork-wrapper.test.js`
- `.github/workflows/ci.yml`

**Problem:**

The wrapper unit tests cover representative mappings, but no Windows runner
installs the tarball and executes npm-generated `.cmd` / `.ps1` shims.

**Suggested fix:**

- Add a Windows CI job that runs:

```powershell
bash scripts/build-release.sh ci
bash scripts/build-npm-package.sh ci
npm install -g .\npm\remork\remork-*.tgz --prefix "$env:TEMP\remork-npm"
& "$env:TEMP\remork-npm\remork.cmd" version
& "$env:TEMP\remork-npm\remork.cmd" setup --help
```

- If Bash is unavailable on Windows CI, split release build/package steps into
  platform-neutral scripts later.

### 8. Top-level README development commands still use old prerelease tag format

**Area:** documentation consistency

**Files:**

- `README.md`
- `README_ZH.md`

**Problem:**

The development sections still show `v0.1.1.beta03`. The npm packaging design
uses semver-friendly tags such as `v0.1.1-beta.4` and npm versions such as
`0.1.1-beta.4`.

**Suggested fix:**

- Update development examples to `v0.1.1-beta.4` or to a placeholder like
  `vX.Y.Z-beta.N`.
- Keep English and Chinese READMEs consistent.

## Excluded / Monitor Only

### Setup preview empty `scp` source

**Status:** likely not a visible user-facing bug.

One review pass noted that `BuildDaemonDeployPlan` can internally create a
command string from an empty `localBin`. The adversarial pass found that setup
does not render `plan.Commands`, and visible daemon dry-run resolves or
validates `localBin` before printing commands. Keep this in mind if future UI
starts rendering `OperationPlan.Commands`, but do not prioritize it as an
active bug now.

## Suggested Fix Order

1. Fix release tag fallback URL.
2. Move local validation before SSH probes.
3. Add npm README security warning.
4. Add beta publish tag guidance.
5. Wire install-level npm smoke into CI.
6. Decide whether to block unsupported client platforms with npm metadata or
   document unsupported Linux client installs.
7. Add Windows npm install smoke.
8. Update old README development tag examples.

## Verification Already Performed by Review Agents

- `npm test` for wrapper tests passed.
- `npm pack --dry-run --json` showed expected packed files.
- macOS temp-prefix npm install smoke ran `remork version`, `remork setup --help`,
  and `remork daemon install --help`.
- Targeted Go daemon/setup tests passed.
- `npm publish --dry-run --json` behavior was checked: prerelease publish needs
  an explicit dist-tag such as `--tag beta`.
- `npm view remork` returned 404 during the docs/UX pass, so the package name
  appeared unpublished at that time.

## Residual Uncertainty

- Windows npm shim behavior has not been run on a real Windows host.
- Real SSH/TUI setup flows were not rerun during the review.
- GitHub Release fallback was reasoned from tag conventions and script output;
  the exact release URL was not network-verified against a newly published
  `v0.1.1-beta.4` release.
