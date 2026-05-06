# Remork npm Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish Remork as `npm install -g remork`, with all supported client binaries and Linux daemon binaries included in the npm package.

**Architecture:** Keep the Go CLI as the only implementation of Remork behavior. Add a Node `bin/remork.js` wrapper that selects the current platform client binary and injects `REMORK_DAEMON_VENDOR_DIR`; extend the Go daemon binary resolver to prefer that vendor directory before cache/download fallback. Add release scripts and docs that produce and smoke-test `npm/remork`.

**Tech Stack:** Go 1.22, Cobra CLI, Bash release scripts, Node.js CommonJS wrapper, npm packaging.

---

## File Structure

- Modify `internal/cli/release_binary.go`: add vendor directory resolution and platform selection helpers.
- Modify `internal/cli/release_binary_test.go`: cover vendor priority, local-bin override, fallback, and unsupported platform errors.
- Modify `internal/cli/commands_daemon.go`: when remote platform detection fails in interactive mode and vendor daemon binaries are available, prompt for `linux-arm64` or `linux-amd64`.
- Modify `internal/cli/commands_daemon_test.go`: cover interactive fallback platform selection and non-interactive error text.
- Modify `cmd/remork/main.go`: normalize leading `v` out of the CLI version string before passing it to `cli.NewRootCommand`.
- Modify `cmd/remork/main_test.go`: cover version normalization without leading `v`.
- Create `npm/remork/bin/remork.js`: thin Node wrapper for platform selection, argument forwarding, and env injection.
- Create `npm/remork/test/remork-wrapper.test.js`: Node unit tests for wrapper mapping and process spawning.
- Create `scripts/build-npm-package.sh`: build the npm package directory from `dist/`.
- Modify `scripts/build-release.sh`: normalize build-time Go version by removing a leading `v` while keeping release URLs based on the tag.
- Modify `.github/workflows/ci.yml`: run npm wrapper tests and npm package dry-run after building release binaries.
- Modify `README.md` and `README_ZH.md`: make npm install the primary install path and keep direct binary installation as fallback.

## Task 1: Add Vendor Daemon Binary Resolution

**Files:**
- Modify: `internal/cli/release_binary.go`
- Modify: `internal/cli/release_binary_test.go`

- [ ] **Step 1: Add failing tests for vendor priority and fallback**

Add these tests to `internal/cli/release_binary_test.go` after `TestResolveReleaseDaemonBinaryLocalBinWinsForDev`:

```go
func TestResolveReleaseDaemonBinaryLocalBinOverridesVendorDir(t *testing.T) {
	vendorDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vendorDir, "remorkd-linux-arm64"), []byte("vendor daemon"), 0o755); err != nil {
		t.Fatalf("write vendor binary: %v", err)
	}
	t.Setenv("REMORK_DAEMON_VENDOR_DIR", vendorDir)

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "0.1.1-beta.4",
		HomeDir:  t.TempDir(),
		Platform: "linux-arm64",
		LocalBin: "/custom/remorkd",
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != "/custom/remorkd" {
		t.Fatalf("path = %q, want explicit local bin", got)
	}
}

func TestResolveReleaseDaemonBinaryUsesVendorDirBeforeDist(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join("dist", "remorkd-linux-arm64"), []byte("dist daemon"), 0o755); err != nil {
		t.Fatalf("write dist binary: %v", err)
	}

	vendorDir := t.TempDir()
	vendorPath := filepath.Join(vendorDir, "remorkd-linux-arm64")
	if err := os.WriteFile(vendorPath, []byte("vendor daemon"), 0o755); err != nil {
		t.Fatalf("write vendor binary: %v", err)
	}
	t.Setenv("REMORK_DAEMON_VENDOR_DIR", vendorDir)

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "0.1.1-beta.4",
		HomeDir:  t.TempDir(),
		Platform: "linux-arm64",
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != vendorPath {
		t.Fatalf("path = %q, want vendor path %q", got, vendorPath)
	}
}

func TestResolveReleaseDaemonBinaryFallsBackWhenVendorBinaryMissing(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	distPath := filepath.Join("dist", "remorkd-linux-amd64")
	if err := os.WriteFile(distPath, []byte("dist daemon"), 0o755); err != nil {
		t.Fatalf("write dist binary: %v", err)
	}
	t.Setenv("REMORK_DAEMON_VENDOR_DIR", t.TempDir())

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "0.1.1-beta.4",
		HomeDir:  t.TempDir(),
		Platform: "linux-amd64",
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != distPath {
		t.Fatalf("path = %q, want dist fallback %q", got, distPath)
	}
}

func TestResolveReleaseDaemonBinaryUnknownNonLinuxPlatformAsksForLinuxPlatform(t *testing.T) {
	_, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "0.1.1-beta.4",
		HomeDir:  t.TempDir(),
		Platform: "darwin-arm64",
	})
	if err == nil {
		t.Fatal("resolveReleaseDaemonBinary returned nil error, want unsupported platform error")
	}
	if !strings.Contains(err.Error(), "pass --platform linux-arm64 or linux-amd64") {
		t.Fatalf("error = %q, want explicit platform guidance", err.Error())
	}
}
```

Add `strings` to the import block.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/cli -run 'TestResolveReleaseDaemonBinary(LocalBinOverridesVendorDir|UsesVendorDirBeforeDist|FallsBackWhenVendorBinaryMissing|UnknownNonLinuxPlatformAsksForLinuxPlatform)'
```

Expected: at least `TestResolveReleaseDaemonBinaryUsesVendorDirBeforeDist` fails because vendor directory resolution is not implemented.

- [ ] **Step 3: Implement vendor directory lookup**

In `internal/cli/release_binary.go`, add:

```go
const daemonVendorDirEnv = "REMORK_DAEMON_VENDOR_DIR"
```

Inside `resolveReleaseDaemonBinary`, after `name := "remorkd-" + platform` and before the `distPath` lookup, add:

```go
if vendorPath := vendorDaemonBinaryPath(os.Getenv(daemonVendorDirEnv), name); vendorPath != "" {
	return vendorPath, nil
}
```

Add this helper near `fileExists`:

```go
func vendorDaemonBinaryPath(vendorDir, name string) string {
	vendorDir = strings.TrimSpace(vendorDir)
	if vendorDir == "" {
		return ""
	}
	path := filepath.Join(vendorDir, name)
	if fileExists(path) {
		return path
	}
	return ""
}
```

Add `strings` to the `release_binary.go` import block.

Update the non-linux platform error in `resolveReleaseDaemonBinary` to:

```go
return "", fmt.Errorf("could not select remorkd platform from %s; pass --platform linux-arm64 or linux-amd64", platform)
```

- [ ] **Step 4: Run resolver tests**

Run:

```bash
go test ./internal/cli -run TestResolveReleaseDaemonBinary
```

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

```bash
git add internal/cli/release_binary.go internal/cli/release_binary_test.go
git commit -m "feat: resolve remorkd from vendor directory"
```

## Task 2: Add Interactive Platform Fallback for Vendor Daemons

**Files:**
- Modify: `internal/cli/commands_daemon.go`
- Modify: `internal/cli/commands_daemon_test.go`

- [ ] **Step 1: Add failing test for platform selection helper**

Add these tests to `internal/cli/commands_daemon_test.go` near the daemon platform detection tests:

```go
func TestChooseDaemonVendorPlatformUsesPromptSelection(t *testing.T) {
	vendorDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vendorDir, "remorkd-linux-arm64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write arm vendor binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "remorkd-linux-amd64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write amd vendor binary: %v", err)
	}

	got, err := chooseDaemonVendorPlatformForTest(vendorDir, "down\nenter\n")
	if err != nil {
		t.Fatalf("chooseDaemonVendorPlatform returned error: %v", err)
	}
	if got != "linux-amd64" {
		t.Fatalf("platform = %q, want linux-amd64", got)
	}
}

func TestChooseDaemonVendorPlatformRejectsMissingVendorBinaries(t *testing.T) {
	_, err := chooseDaemonVendorPlatformForTest(t.TempDir(), "enter\n")
	if err == nil {
		t.Fatal("chooseDaemonVendorPlatform returned nil error, want missing vendor binary error")
	}
	if !strings.Contains(err.Error(), "vendor remorkd binaries are not available") {
		t.Fatalf("error = %q, want missing vendor guidance", err.Error())
	}
}
```

Add this test helper in the same file:

```go
func chooseDaemonVendorPlatformForTest(vendorDir, keys string) (string, error) {
	var in bytes.Buffer
	for _, key := range strings.Split(strings.TrimSuffix(keys, "\n"), "\n") {
		switch key {
		case "down":
			in.WriteByte('j')
		case "enter":
			in.WriteByte('\n')
		default:
			in.WriteString(key)
		}
	}
	var out bytes.Buffer
	return chooseDaemonVendorPlatform(&in, &out, output.ColorNever, vendorDir)
}
```

Use imports that already exist in `commands_daemon_test.go`; add `remork/internal/output` only if it is not already imported.

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/cli -run TestChooseDaemonVendorPlatform
```

Expected: FAIL because `chooseDaemonVendorPlatform` is not defined.

- [ ] **Step 3: Implement platform selection helper**

In `internal/cli/commands_daemon.go`, add:

```go
func chooseDaemonVendorPlatform(in io.Reader, out io.Writer, color output.ColorMode, vendorDir string) (string, error) {
	items := []tui.CommandItem{}
	for _, platform := range []string{"linux-arm64", "linux-amd64"} {
		name := "remorkd-" + platform
		if vendorDaemonBinaryPath(vendorDir, name) != "" {
			items = append(items, tui.CommandItem{
				Name:        platform,
				Description: "Use " + name + " from the packaged npm vendor directory",
				Args:        []string{platform},
			})
		}
	}
	if len(items) == 0 {
		return "", fmt.Errorf("vendor remorkd binaries are not available; pass --platform linux-arm64 or linux-amd64")
	}
	model := tui.NewCommandMenu("Select server daemon platform", items)
	model.Color = color
	menu, err := tui.RunCommandMenu(model, tea.WithInput(in), tea.WithOutput(out))
	if err != nil {
		return "", err
	}
	if menu.Canceled() || !menu.Submitted() || len(menu.SelectedArgs()) == 0 {
		return "", fmt.Errorf("daemon platform selection cancelled")
	}
	return menu.SelectedArgs()[0], nil
}
```

Ensure `commands_daemon.go` has the required imports. It already uses `io`, `fmt`, `output`, and `tui`; add `tea "github.com/charmbracelet/bubbletea"` if it is not already present.

- [ ] **Step 4: Use helper when remote platform detection fails interactively**

In `prepareAndRunDaemonDeploy`, find this block:

```go
platform, err := detectRemoteDaemonPlatform(cmd.Context(), runner, deploySSHTarget(deploy))
if err != nil {
	reporter.FailMessage("remote platform detection failed")
	return err
}
```

Replace it with:

```go
platform, err := detectRemoteDaemonPlatform(cmd.Context(), runner, deploySSHTarget(deploy))
if err != nil {
	reporter.FailMessage("remote platform detection failed")
	if mode.RichOutput {
		chosen, chooseErr := chooseDaemonVendorPlatform(cmd.InOrStdin(), cmd.ErrOrStderr(), commandColorMode(cmd), os.Getenv(daemonVendorDirEnv))
		if chooseErr == nil {
			platform = chosen
		} else {
			return fmt.Errorf("%w; %v", err, chooseErr)
		}
	} else {
		return err
	}
}
```

This keeps non-interactive behavior explicit while giving setup/TUI users a choice when vendor daemon binaries are packaged.

- [ ] **Step 5: Run daemon command tests**

Run:

```bash
go test ./internal/cli -run 'TestChooseDaemonVendorPlatform|TestDaemonInstallAutoDetectsRemotePlatformForReleaseBinary'
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

```bash
git add internal/cli/commands_daemon.go internal/cli/commands_daemon_test.go
git commit -m "feat: prompt for packaged daemon platform"
```

## Task 3: Normalize Version Strings

**Files:**
- Modify: `cmd/remork/main.go`
- Modify: `cmd/remork/main_test.go`
- Modify: `scripts/build-release.sh`

- [ ] **Step 1: Add failing version normalization test**

Add to `cmd/remork/main_test.go`:

```go
func TestDisplayVersionTrimsLeadingV(t *testing.T) {
	got := displayVersion("v0.1.1-beta.4")
	if got != "0.1.1-beta.4" {
		t.Fatalf("displayVersion = %q, want 0.1.1-beta.4", got)
	}
}

func TestDisplayVersionLeavesDevUnchanged(t *testing.T) {
	got := displayVersion("dev")
	if got != "dev" {
		t.Fatalf("displayVersion = %q, want dev", got)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./cmd/remork -run TestDisplayVersion
```

Expected: FAIL because `displayVersion` is not defined.

- [ ] **Step 3: Implement version normalization**

In `cmd/remork/main.go`, add `strings` to imports and add:

```go
func displayVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "v") && len(raw) > 1 && raw[1] >= '0' && raw[1] <= '9' {
		return raw[1:]
	}
	return raw
}
```

Change `main()` to:

```go
func main() {
	if err := cli.NewRootCommand(cli.Options{Version: displayVersion(version)}).Execute(); err != nil {
		if !isSilentError(err) {
			cli.WriteCommandError(os.Stderr, err)
		}
		os.Exit(commandExitCode(err))
	}
}
```

- [ ] **Step 4: Keep build script release URLs tagged but Go version unprefixed**

In `scripts/build-release.sh`, after `version="${1:-dev}"`, add:

```bash
binary_version="$version"
if [[ "$binary_version" == v[0-9]* ]]; then
  binary_version="${binary_version#v}"
fi
```

Change the `go build` ldflags line from:

```bash
go build -trimpath -ldflags "-s -w -X main.version=$version" \
```

to:

```bash
go build -trimpath -ldflags "-s -w -X main.version=$binary_version" \
```

Keep release URLs and README links using `$version`, because GitHub tags retain `v`.

- [ ] **Step 5: Run version and release build tests**

Run:

```bash
go test ./cmd/remork -run TestDisplayVersion
bash scripts/build-release.sh v0.1.1-beta.4
./dist/remork-darwin-arm64 version
```

Expected on Apple Silicon macOS: `remork 0.1.1-beta.4`. On Intel macOS, use `./dist/remork-darwin-amd64 version`.

- [ ] **Step 6: Commit Task 3**

```bash
git add cmd/remork/main.go cmd/remork/main_test.go scripts/build-release.sh
git commit -m "feat: use npm friendly remork versions"
```

## Task 4: Add Node Wrapper and Unit Tests

**Files:**
- Create: `npm/remork/bin/remork.js`
- Create: `npm/remork/test/remork-wrapper.test.js`
- Create: `npm/remork/package.json`
- Create: `npm/remork/README.md`

- [ ] **Step 1: Create minimal npm package metadata for wrapper tests**

Create `npm/remork/package.json`:

```json
{
  "name": "remork",
  "version": "0.0.0-dev",
  "private": true,
  "description": "Remote workspace control for private servers",
  "bin": {
    "remork": "bin/remork.js"
  },
  "scripts": {
    "test": "node --test test/*.test.js"
  },
  "files": [
    "bin/",
    "vendor/",
    "README.md"
  ],
  "engines": {
    "node": ">=18"
  },
  "license": "UNLICENSED"
}
```

Create `npm/remork/README.md`:

````markdown
# Remork

Remote workspace control for private servers.

```bash
npm install -g remork
remork setup
```

This npm package includes the Remork client binaries for macOS and Windows plus
Linux `remorkd` daemon binaries used by setup.
````

- [ ] **Step 2: Add failing wrapper tests**

Create `npm/remork/test/remork-wrapper.test.js`:

```js
const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");

const wrapper = require("../bin/remork.js");

test("selects macOS arm64 client binary", () => {
  assert.equal(
    wrapper.clientBinaryName({ platform: "darwin", arch: "arm64" }),
    "remork-darwin-arm64",
  );
});

test("selects Windows x64 client binary", () => {
  assert.equal(
    wrapper.clientBinaryName({ platform: "win32", arch: "x64" }),
    "remork-windows-amd64.exe",
  );
});

test("injects daemon vendor directory", () => {
  const env = wrapper.childEnv({ FOO: "bar" }, "/tmp/remork-package");
  assert.equal(env.FOO, "bar");
  assert.equal(env.REMORK_DAEMON_VENDOR_DIR, path.join("/tmp/remork-package", "vendor"));
});

test("builds spawn plan with args and inherited stdio", () => {
  const plan = wrapper.spawnPlan({
    packageRoot: "/pkg",
    argv: ["setup", "--help"],
    platform: "darwin",
    arch: "arm64",
    env: { PATH: "/bin" },
  });

  assert.equal(plan.command, path.join("/pkg", "vendor", "remork-darwin-arm64"));
  assert.deepEqual(plan.args, ["setup", "--help"]);
  assert.equal(plan.options.stdio, "inherit");
  assert.equal(plan.options.env.REMORK_DAEMON_VENDOR_DIR, path.join("/pkg", "vendor"));
});

test("rejects unsupported client platform", () => {
  assert.throws(
    () => wrapper.clientBinaryName({ platform: "linux", arch: "x64" }),
    /unsupported Remork client platform/,
  );
});
```

- [ ] **Step 3: Run wrapper tests and verify they fail**

Run:

```bash
npm --prefix npm/remork test
```

Expected: FAIL because `bin/remork.js` does not exist.

- [ ] **Step 4: Implement wrapper**

Create `npm/remork/bin/remork.js`:

```js
#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const childProcess = require("node:child_process");

function clientBinaryName(runtime = process) {
  const platform = runtime.platform;
  const arch = runtime.arch;
  if (platform === "darwin" && arch === "arm64") return "remork-darwin-arm64";
  if (platform === "darwin" && arch === "x64") return "remork-darwin-amd64";
  if (platform === "win32" && arch === "arm64") return "remork-windows-arm64.exe";
  if (platform === "win32" && arch === "x64") return "remork-windows-amd64.exe";
  throw new Error(`unsupported Remork client platform: ${platform}-${arch}`);
}

function packageRootFromFilename(filename = __filename) {
  return path.resolve(path.dirname(filename), "..");
}

function childEnv(baseEnv = process.env, packageRoot = packageRootFromFilename()) {
  return {
    ...baseEnv,
    REMORK_DAEMON_VENDOR_DIR: path.join(packageRoot, "vendor"),
  };
}

function spawnPlan({
  packageRoot = packageRootFromFilename(),
  argv = process.argv.slice(2),
  platform = process.platform,
  arch = process.arch,
  env = process.env,
} = {}) {
  const command = path.join(packageRoot, "vendor", clientBinaryName({ platform, arch }));
  return {
    command,
    args: argv,
    options: {
      stdio: "inherit",
      env: childEnv(env, packageRoot),
    },
  };
}

function main() {
  let plan;
  try {
    plan = spawnPlan();
    if (!fs.existsSync(plan.command)) {
      throw new Error(`Remork client binary is missing: ${plan.command}`);
    }
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }

  const child = childProcess.spawn(plan.command, plan.args, plan.options);
  child.on("error", (err) => {
    console.error(err.message);
    process.exit(1);
  });
  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code ?? 1);
  });
}

module.exports = {
  clientBinaryName,
  childEnv,
  packageRootFromFilename,
  spawnPlan,
  main,
};

if (require.main === module) {
  main();
}
```

- [ ] **Step 5: Run wrapper tests**

Run:

```bash
npm --prefix npm/remork test
```

Expected: PASS.

- [ ] **Step 6: Commit Task 4**

```bash
git add npm/remork/package.json npm/remork/README.md npm/remork/bin/remork.js npm/remork/test/remork-wrapper.test.js
git commit -m "feat: add npm remork wrapper"
```

## Task 5: Add npm Package Build Script

**Files:**
- Create: `scripts/build-npm-package.sh`
- Modify: `npm/remork/package.json`
- Modify: `npm/remork/README.md`

- [ ] **Step 1: Add failing build script smoke command**

Run:

```bash
bash scripts/build-release.sh v0.1.1-beta.4
bash scripts/build-npm-package.sh v0.1.1-beta.4
```

Expected: FAIL because `scripts/build-npm-package.sh` does not exist.

- [ ] **Step 2: Implement package build script**

Create `scripts/build-npm-package.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

tag="${1:-dev}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$repo_root/dist"
pkg_dir="$repo_root/npm/remork"
vendor_dir="$pkg_dir/vendor"

npm_version="$tag"
if [[ "$npm_version" == v[0-9]* ]]; then
  npm_version="${npm_version#v}"
fi
if [[ "$npm_version" == "dev" ]]; then
  npm_version="0.0.0-dev"
fi

required=(
  remork-darwin-arm64
  remork-darwin-amd64
  remork-windows-arm64.exe
  remork-windows-amd64.exe
  remorkd-linux-arm64
  remorkd-linux-amd64
)

for asset in "${required[@]}"; do
  if [[ ! -f "$dist_dir/$asset" ]]; then
    echo "missing dist asset: $dist_dir/$asset" >&2
    echo "run scripts/build-release.sh $tag first" >&2
    exit 1
  fi
done

rm -rf "$vendor_dir"
mkdir -p "$vendor_dir"
for asset in "${required[@]}"; do
  cp "$dist_dir/$asset" "$vendor_dir/$asset"
done
chmod 0755 "$vendor_dir"/remork-* "$vendor_dir"/remorkd-*

cat > "$pkg_dir/package.json" <<EOF
{
  "name": "remork",
  "version": "$npm_version",
  "description": "Remote workspace control for private servers",
  "bin": {
    "remork": "bin/remork.js"
  },
  "scripts": {
    "test": "node --test test/*.test.js"
  },
  "files": [
    "bin/",
    "vendor/",
    "README.md"
  ],
  "engines": {
    "node": ">=18"
  },
  "repository": {
    "type": "git",
    "url": "git+https://github.com/zhangtao0408/Remork.git"
  },
  "homepage": "https://github.com/zhangtao0408/Remork#readme",
  "bugs": {
    "url": "https://github.com/zhangtao0408/Remork/issues"
  },
  "license": "UNLICENSED"
}
EOF

cat > "$pkg_dir/README.md" <<EOF
# Remork

Remote workspace control for private servers.

## Install

\`\`\`bash
npm install -g remork
remork setup
\`\`\`

This package includes Remork client binaries for macOS and Windows plus Linux
\`remorkd\` daemon binaries used by \`remork setup\`.

Supported client platforms:

- macOS arm64
- macOS amd64
- Windows arm64
- Windows amd64

Supported server daemon platforms:

- Linux arm64
- Linux amd64

For full documentation, see https://github.com/zhangtao0408/Remork.
EOF

(cd "$pkg_dir" && npm pack --dry-run)
```

Make it executable:

```bash
chmod 0755 scripts/build-npm-package.sh
```

- [ ] **Step 3: Run build script**

Run:

```bash
bash scripts/build-release.sh v0.1.1-beta.4
bash scripts/build-npm-package.sh v0.1.1-beta.4
```

Expected: PASS and `npm pack --dry-run` lists only `bin/remork.js`, `vendor/*`, `README.md`, and `package.json`.

- [ ] **Step 4: Verify generated package version**

Run:

```bash
node -e 'console.log(require("./npm/remork/package.json").version)'
```

Expected: `0.1.1-beta.4`.

- [ ] **Step 5: Commit Task 5**

```bash
git add scripts/build-npm-package.sh npm/remork/package.json npm/remork/README.md npm/remork/vendor
git commit -m "feat: build bundled npm package"
```

## Task 6: Add Package Smoke Tests and CI Wiring

**Files:**
- Modify: `.github/workflows/ci.yml`
- Create: `scripts/smoke-npm-package.sh`

- [ ] **Step 1: Create smoke script**

Create `scripts/smoke-npm-package.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pkg_dir="$repo_root/npm/remork"
tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

(cd "$pkg_dir" && npm test)
(cd "$pkg_dir" && npm pack --dry-run)

if [[ "$(uname -s)" == "Darwin" ]]; then
  tarball="$(cd "$pkg_dir" && npm pack --silent)"
  npm install -g "$pkg_dir/$tarball" --prefix "$tmp_dir/npm-global"
  "$tmp_dir/npm-global/bin/remork" version
  "$tmp_dir/npm-global/bin/remork" setup --help >/dev/null
  "$tmp_dir/npm-global/bin/remork" daemon install --help >/dev/null
fi
```

Make it executable:

```bash
chmod 0755 scripts/smoke-npm-package.sh
```

- [ ] **Step 2: Run smoke script locally**

Run:

```bash
bash scripts/build-release.sh v0.1.1-beta.4
bash scripts/build-npm-package.sh v0.1.1-beta.4
bash scripts/smoke-npm-package.sh
```

Expected on macOS: PASS and `remork version` prints `remork 0.1.1-beta.4`.

- [ ] **Step 3: Wire CI**

Modify `.github/workflows/ci.yml` after `Build release binaries`:

```yaml
      - name: Build npm package
        run: bash scripts/build-npm-package.sh ci

      - name: Test npm wrapper
        run: npm --prefix npm/remork test
```

Do not add npm publish to CI in this task.

- [ ] **Step 4: Run CI-equivalent commands locally**

Run:

```bash
go test -count=1 ./...
go vet ./...
bash scripts/build-release.sh ci
bash scripts/build-npm-package.sh ci
npm --prefix npm/remork test
```

Expected: PASS.

- [ ] **Step 5: Commit Task 6**

```bash
git add .github/workflows/ci.yml scripts/smoke-npm-package.sh
git commit -m "ci: validate npm package build"
```

## Task 7: Update Documentation for npm Install

**Files:**
- Modify: `README.md`
- Modify: `README_ZH.md`

- [ ] **Step 1: Update English README install section**

In `README.md`, replace the top of `## Install` through the Windows PATH paragraph with:

````markdown
## Install

Install the Remork client with npm:

```bash
npm install -g remork
remork version
```

Then start the product setup flow:

```bash
remork setup
```

The npm package includes macOS and Windows client binaries plus Linux `remorkd`
daemon binaries used by setup. Setup can prepare or update a server without a
second binary download.

Manual binary installation remains available from GitHub Releases when npm is
not available.
````

- [ ] **Step 2: Update Chinese README install section**

In `README_ZH.md`, replace the top of `## 安装` through the Windows PATH paragraph with:

````markdown
## 安装

使用 npm 安装 Remork client：

```bash
npm install -g remork
remork version
```

然后进入产品化设置流程：

```bash
remork setup
```

npm 包内已经包含 macOS 和 Windows client binary，也包含 setup 安装远端服务时需要的 Linux `remorkd` binary。正常 setup 不需要再二次下载 daemon。

如果环境里没有 npm，也可以继续从 GitHub Releases 手动下载 binary。
````

- [ ] **Step 3: Run markdown grep checks**

Run:

```bash
rg -n "npm install -g remork|GitHub Releases|remork setup" README.md README_ZH.md
```

Expected: both READMEs mention npm install and setup.

- [ ] **Step 4: Commit Task 7**

```bash
git add README.md README_ZH.md
git commit -m "docs: make npm install primary"
```

## Task 8: Final Verification

**Files:**
- No source edits expected.

- [ ] **Step 1: Run full test suite**

Run:

```bash
go test ./...
go vet ./...
```

Expected: PASS.

- [ ] **Step 2: Build release and npm package with npm-friendly tag**

Run:

```bash
bash scripts/build-release.sh v0.1.1-beta.4
bash scripts/build-npm-package.sh v0.1.1-beta.4
```

Expected: PASS.

- [ ] **Step 3: Run npm smoke**

Run:

```bash
bash scripts/smoke-npm-package.sh
```

Expected on macOS: PASS and the installed package command can run `version`, `setup --help`, and `daemon install --help`.

- [ ] **Step 4: Inspect package contents**

Run:

```bash
cd npm/remork
npm pack --dry-run
```

Expected: package contents include `bin/remork.js`, the six `vendor/` binaries, `README.md`, and `package.json`. It should not include repository docs, tests, `.superpowers`, or local config files.

- [ ] **Step 5: Commit final verification note if needed**

If final verification requires only command execution, do not create a commit. If it reveals a documentation or script correction, commit only that correction:

```bash
git add README.md README_ZH.md scripts/build-npm-package.sh scripts/smoke-npm-package.sh .github/workflows/ci.yml npm/remork/package.json npm/remork/README.md npm/remork/bin/remork.js
git commit -m "fix: polish npm package release checks"
```

## Manual Publish Runbook

Do not publish automatically during implementation. After the user approves the built package:

```bash
npm login
cd npm/remork
npm publish --tag beta
```

Before publishing, run:

```bash
npm whoami
npm pack --dry-run
```

Expected: `npm whoami` prints the user's npm account, and the dry-run output contains only intended package files.

For prereleases, `scripts/publish-npm.sh npm/remork` applies `--tag beta` by default.
