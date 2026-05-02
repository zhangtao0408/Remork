# Remork Multi-Workspace Daemon And Install UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make one `remorkd` endpoint able to manage any workspace directory under an allowed server-side base directory, make client-driven daemon install durable by default, and rewrite the README around clear user concepts.

**Architecture:** Treat daemon `--root` as an allowed base root, not as the one workspace root. `remork init HOST:/remote/workspace` binds the current local directory to a concrete workspace under an allowed base root, while daemon APIs validate every requested workspace root dynamically. `remork daemon install` becomes the primary client-side install path: it copies or downloads a prebuilt `remorkd`, stores it under the remote user home, starts it with durable log/pid paths, configures the local host URL when provided, and verifies `/status`.

**Tech Stack:** Go CLI and HTTP daemon, Cobra, existing `internal/client`, `internal/paths`, `internal/ops`, `internal/workspace`, Go unit tests, remote smoke tests over SSH.

---

## Current Findings

- `remork host add` only stores a local alias and URL in `~/.remork/config.json`; it does not change remote daemon roots.
- `remork init` currently checks the daemon `/status` roots with exact string equality in `internal/cli/commands_init.go`.
- `remorkd` currently checks every API request root with exact string equality in `internal/daemon/server.go`.
- `remorkd` operation log stores are pre-created only for the exact startup roots, so arbitrary child workspace roots also require dynamic operation store resolution.
- `remork daemon install` exists, but it defaults to `/tmp/remorkd`, `/tmp/remorkd.log`, and `/tmp/remorkd.pid`, which is not a durable product install path.
- The README uses `lab-a`, `project-a`, `/data/project-a`, and `http://remork-daemon.example.internal:17731` without clearly defining which value is a local alias, an SSH target, a daemon HTTP URL, a daemon allowed root, or a workspace root.

## Target User Model

- **Remork host:** A local alias for one remote daemon endpoint, for example `remork-host-a`.
- **SSH target:** How the installer copies and starts `remorkd`, for example `remork-host-a` or `user@remork-daemon-a.example.internal`.
- **Daemon URL:** How local Remork talks to the daemon after install, for example `http://remork-daemon-a.example.internal:17731`. This is HTTP, not SSH.
- **Allowed root:** The server-side boundary that `remorkd` is allowed to serve, for example `/home/me`.
- **Workspace root:** The actual project directory bound to a local working copy, for example `/home/me/project`.
- **Local working copy:** The local folder where the human or Agent edits files, for example `~/remork/Wan22_Adapt`.

## Files

- Modify: `cmd/remorkd/main.go` to describe `--root` as an allowed base root and normalize configured roots before server creation.
- Modify: `internal/daemon/server.go` to allow requested workspace roots inside configured allowed base roots and create operation stores dynamically.
- Modify: `internal/daemon/server_test.go` to cover exact root, child workspace root, trailing slash normalization, sibling prefix rejection, `..` rejection, symlink escape rejection, operations logging for dynamic child roots, and missing root rejection.
- Modify: `internal/cli/commands_init.go` to use shared "root is within advertised base root" semantics and probe `Manifest(root, ".")` before writing the binding.
- Modify: `internal/cli/commands_doctor.go` to use the same root containment semantics and improve fix text.
- Modify: `internal/cli/root_test.go` to cover init under an advertised parent root and rejection of sibling paths.
- Modify: `internal/cli/commands_daemon.go` to use durable remote paths, optional host config write, optional binary download/cache, platform detection, and verification.
- Modify: `internal/cli/commands_daemon_test.go` to cover durable paths, install command ordering, host config write behavior, platform detection, and release binary download/cache selection.
- Create: `internal/remoteroot/remoteroot.go` for path normalization and containment shared by CLI and daemon.
- Create: `internal/remoteroot/remoteroot_test.go` for pure containment edge cases.
- Create: `internal/cli/release_binary.go` for local release asset lookup/download/cache logic.
- Create: `internal/cli/release_binary_test.go` for cache-hit, cache-miss, HTTP failure, and unsupported platform tests.
- Modify: `README.md` and `README_ZH.md` to use the target user model and a concrete end-to-end workflow.
- Modify: `docs/remork-api.md` to define `roots` as allowed base roots and `root=<workspace-root>` as the requested workspace.
- Modify: `skills/remork/SKILL.md` to teach agents the new install/init flow.

---

### Task 1: Shared Remote Root Semantics

**Files:**
- Create: `internal/remoteroot/remoteroot.go`
- Create: `internal/remoteroot/remoteroot_test.go`

- [ ] **Step 1: Write failing containment tests**

Add tests that define the intended semantics:

```go
func TestContainsAllowsExactAndChildren(t *testing.T) {
	allowed, err := Normalize("/home/me/")
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{
		"/home/me",
		"/home/me/project",
		"/home/me/projects/a",
	} {
		ok, err := Contains([]Root{allowed}, candidate)
		if err != nil {
			t.Fatalf("Contains(%q): %v", candidate, err)
		}
		if !ok {
			t.Fatalf("Contains(%q) = false, want true", candidate)
		}
	}
}

func TestContainsRejectsSiblingPrefixAndRelativePaths(t *testing.T) {
	allowed, err := Normalize("/home/me")
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{
		"/home/me_other",
		"/home/me/../root",
		"home/me",
		"",
	} {
		ok, err := Contains([]Root{allowed}, candidate)
		if err == nil && ok {
			t.Fatalf("Contains(%q) = true, want false", candidate)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/remoteroot
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement pure lexical normalization and containment**

Implement:

```go
package remoteroot

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Root struct {
	Raw   string
	Clean string
}

func Normalize(root string) (Root, error) {
	if root == "" {
		return Root{}, fmt.Errorf("root is required")
	}
	if !filepath.IsAbs(root) {
		return Root{}, fmt.Errorf("root %q must be absolute", root)
	}
	clean := filepath.Clean(root)
	return Root{Raw: root, Clean: clean}, nil
}

func NormalizeMany(roots []string) ([]Root, error) {
	out := make([]Root, 0, len(roots))
	for _, root := range roots {
		normalized, err := Normalize(root)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	return out, nil
}

func Contains(allowed []Root, candidate string) (bool, error) {
	requested, err := Normalize(candidate)
	if err != nil {
		return false, err
	}
	for _, base := range allowed {
		if requested.Clean == base.Clean {
			return true, nil
		}
		prefix := strings.TrimRight(base.Clean, string(filepath.Separator)) + string(filepath.Separator)
		if strings.HasPrefix(requested.Clean, prefix) {
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/remoteroot
```

Expected: PASS.

### Task 2: Daemon Allows Dynamic Workspace Roots Under Allowed Bases

**Files:**
- Modify: `internal/daemon/server.go`
- Modify: `internal/daemon/server_test.go`

- [ ] **Step 1: Write failing daemon tests**

Add tests:

```go
func TestManifestAllowsChildWorkspaceUnderAllowedRoot(t *testing.T) {
	base := t.TempDir()
	child := filepath.Join(base, "project")
	mustWrite(t, filepath.Join(child, "README.md"), []byte("hello"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{base}}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest?root=" + url.QueryEscape(child) + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
}

func TestManifestRejectsSiblingOfAllowedRoot(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "user")
	sibling := filepath.Join(parent, "user2")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(Config{Roots: []string{base}}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest?root=" + url.QueryEscape(sibling) + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
}

func TestOperationsUsesChildWorkspaceLog(t *testing.T) {
	base := t.TempDir()
	child := filepath.Join(base, "repo")
	mustWrite(t, filepath.Join(child, "a.txt"), []byte("a"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{base}}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest?root=" + url.QueryEscape(child) + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	_ = resp.Body.Close()

	opsResp, err := http.Get(srv.URL + "/operations?root=" + url.QueryEscape(child) + "&limit=10")
	if err != nil {
		t.Fatalf("operations: %v", err)
	}
	defer opsResp.Body.Close()
	if opsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(opsResp.Body)
		t.Fatalf("status %d body %s", opsResp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(child, ".remork", "log", "operations.jsonl")); err != nil {
		t.Fatalf("missing child operation log: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/daemon -run 'TestManifestAllowsChildWorkspaceUnderAllowedRoot|TestManifestRejectsSiblingOfAllowedRoot|TestOperationsUsesChildWorkspaceLog'
```

Expected: first and third tests fail because daemon only allows exact roots.

- [ ] **Step 3: Implement dynamic allowed root checks and operation stores**

Change `Server` to keep normalized allowed roots and a mutex-protected store map:

```go
type Server struct {
	cfg             Config
	allowedRoots    []remoteroot.Root
	mux             *http.ServeMux
	ptyManager      *ptysession.Manager
	operationMu     sync.Mutex
	operationStores map[string]ops.Store
}
```

In `NewServer`, normalize roots once. In `allowedRoot`, call `remoteroot.Contains`. In `operationStore(root)`, return nil when not allowed, otherwise create `ops.NewJSONLStore(operationLogPath(filepath.Clean(root)))` on demand.

- [ ] **Step 4: Run daemon tests**

Run:

```bash
go test ./internal/daemon
```

Expected: PASS.

### Task 3: Init And Doctor Use Parent-Allowed Workspace Semantics

**Files:**
- Modify: `internal/cli/commands_init.go`
- Modify: `internal/cli/commands_doctor.go`
- Modify: `internal/cli/root_test.go`

- [ ] **Step 1: Write failing init tests**

Add:

```go
func TestInitAcceptsWorkspaceUnderAdvertisedParentRoot(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: fakeDaemonProbe{Roots: []string{"/home/me"}},
	})
	if _, err := executeCommand(cmd, "host", "add", "remote", "--url", "http://remork-daemon.example.internal:17731"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "remote:/home/me/project"); err != nil {
		t.Fatalf("init should accept child workspace: %v", err)
	}
}

func TestInitRejectsWorkspaceSiblingOfAdvertisedRoot(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: fakeDaemonProbe{Roots: []string{"/home/me"}},
	})
	if _, err := executeCommand(cmd, "host", "add", "remote", "--url", "http://remork-daemon.example.internal:17731"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "remote:/home/me_other/repo"); err == nil {
		t.Fatal("init should reject sibling path")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli -run 'TestInitAcceptsWorkspaceUnderAdvertisedParentRoot|TestInitRejectsWorkspaceSiblingOfAdvertisedRoot'
```

Expected: child workspace test fails.

- [ ] **Step 3: Use shared containment in init and doctor**

Replace exact checks with:

```go
ok, err := remoteRootAdvertised(status.Roots, remoteRoot)
if err != nil {
	return err
}
if !ok {
	return fmt.Errorf("remote workspace %q is outside the allowed roots advertised by host %q: %s", remoteRoot, hostName, strings.Join(status.Roots, ", "))
}
```

Use equivalent helper in doctor. Update doctor fix text to:

```text
restart remorkd with --root set to this workspace or to a parent directory such as /home/me
```

- [ ] **Step 4: Run CLI tests**

Run:

```bash
go test ./internal/cli
```

Expected: PASS.

### Task 4: Durable Client-Driven Daemon Install

**Files:**
- Modify: `internal/cli/commands_daemon.go`
- Modify: `internal/cli/commands_daemon_test.go`
- Create: `internal/cli/release_binary.go`
- Create: `internal/cli/release_binary_test.go`

- [ ] **Step 1: Change install defaults in tests first**

Update tests to expect:

```text
remote binary: ~/.local/bin/remorkd
remote pid:    ~/.remork/run/remorkd.pid
remote log:    ~/.remork/log/remorkd.log
```

Add a test that `remoteStartCommand` creates directories and stops the old daemon before starting:

```go
wantParts := []string{
	"mkdir -p \"$HOME/.local/bin\" \"$HOME/.remork/run\" \"$HOME/.remork/log\"",
	"if [ -f \"$HOME/.remork/run/remorkd.pid\" ]; then kill \"$(cat \"$HOME/.remork/run/remorkd.pid\")\" 2>/dev/null || true; fi",
	"nohup \"$HOME/.local/bin/remorkd\" --root '/home/me' --addr '0.0.0.0:17731'",
	">$HOME/.remork/log/remorkd.log",
	"echo $! > \"$HOME/.remork/run/remorkd.pid\"",
}
```

- [ ] **Step 2: Run daemon CLI tests to verify failure**

Run:

```bash
go test ./internal/cli -run 'TestRunDaemonDeploy|TestDaemon'
```

Expected: FAIL because defaults still use `/tmp`.

- [ ] **Step 3: Implement durable remote paths**

Change `daemonDeployOptions` defaults:

```go
remoteBin: "~/.local/bin/remorkd"
pidFile: "$HOME/.remork/run/remorkd.pid"
logFile: "$HOME/.remork/log/remorkd.log"
```

Update copy/start plan to:

```bash
ssh HOST 'mkdir -p "$HOME/.local/bin" "$HOME/.remork/run" "$HOME/.remork/log"'
scp LOCAL_BIN HOST:~/.local/bin/remorkd
ssh HOST 'chmod 0755 "$HOME/.local/bin/remorkd"'
ssh HOST 'if [ -f "$HOME/.remork/run/remorkd.pid" ]; then kill "$(cat "$HOME/.remork/run/remorkd.pid")" 2>/dev/null || true; fi; nohup "$HOME/.local/bin/remorkd" --root ...'
```

- [ ] **Step 4: Add optional URL config and status verification**

Add flags:

```text
--url http://HOST:17731
--no-proxy
--token-env ENV
--verify
```

Behavior:

- If `--url` is provided, write or update `~/.remork/config.json` for the host after a successful install.
- If `--verify` is true, call `remork daemon status HOST` equivalent after host config is written.
- If `--url` is omitted and the host config exists, verify against existing URL.
- If neither exists, print the exact `remork host add` command to run.

- [ ] **Step 5: Add binary resolution for users who installed only the macOS client**

Implement:

```go
func resolveDaemonBinary(version, platform, explicitPath, cacheDir string, downloader assetDownloader) (string, error)
```

Priority:

1. `--local-bin`
2. `dist/remorkd-<platform>` if present
3. `$HOME/.cache/remork/releases/<version>/remorkd-<platform>` if present
4. Download from `https://github.com/zhangtao0408/Remork/releases/download/<version>/remorkd-<platform>` into the cache

Reject `version == "dev"` unless `--local-bin` is provided.

- [ ] **Step 6: Run focused tests**

Run:

```bash
go test ./internal/cli -run 'TestRunDaemonDeploy|TestDaemon|TestResolveDaemonBinary'
```

Expected: PASS.

### Task 5: README And Skill Rewrite

**Files:**
- Modify: `README.md`
- Modify: `README_ZH.md`
- Modify: `docs/remork-api.md`
- Modify: `skills/remork/SKILL.md`

- [ ] **Step 1: Rewrite the quickstart around concrete concepts**

Use this shape:

```markdown
## Mental Model

- Remork host = local nickname for a daemon endpoint.
- SSH target = how Remork installs the daemon.
- Daemon URL = HTTP URL used after install.
- Allowed root = server-side safety boundary.
- Workspace root = project directory you bind locally.
- Local working copy = local folder you edit.
```

- [ ] **Step 2: Replace `lab-a` and `project-a` in the first workflow**

Use concrete placeholders:

```bash
remork daemon install remork-host-a \
  --ssh remork-host-a \
  --url http://remork-daemon-a.example.internal:17731 \
  --root /home/me \
  --platform linux-arm64 \
  --execute --yes \
  --no-proxy

mkdir -p ~/remork/Wan22_Adapt
cd ~/remork/Wan22_Adapt
remork init remork-host-a:/home/me/project
remork sync
remork status
```

Explain that `/home/me` is the allowed root and `/home/me/project` is the workspace root.

- [ ] **Step 3: Move low-level API and manual deployment to later sections**

The first screen should not require users to understand manifests, operation logs, `/tmp`, raw curl calls, or daemon internals.

- [ ] **Step 4: Run README command sanity checks**

Run:

```bash
rg -n "lab-a|project-a|/tmp/remorkd|10\\.0\\.0\\.12|/data/project-a" README.md README_ZH.md
```

Expected: no matches in quickstart; any remaining matches must be in clearly labeled advanced examples.

### Task 6: End-to-End Verification

**Files:**
- Modify: `docs/remork-product-v1-validation.md`

- [ ] **Step 1: Local unit validation**

Run:

```bash
go test -count=1 ./...
```

Expected: PASS.

- [ ] **Step 2: Build release binaries**

Run:

```bash
scripts/build-release.sh v0.1.0
(cd dist && shasum -a 256 -c checksums.txt)
```

Expected: PASS.

- [ ] **Step 3: Remote install smoke on `remork-host-a`**

Run from local:

```bash
remork daemon install remork-host-a \
  --ssh remork-host-a \
  --url http://remork-daemon-a.example.internal:17731 \
  --root /home/me \
  --platform linux-arm64 \
  --execute --yes \
  --no-proxy
```

Then:

```bash
remork daemon status remork-host-a
```

Expected: status lists `/home/me` as an allowed root.

- [ ] **Step 4: Remote child workspace init smoke**

Run:

```bash
tmp_local="$(mktemp -d)"
cd "$tmp_local"
remork init remork-host-a:/home/me/project
remork sync --quiet
remork status
remork run -- pwd
remork log --limit 5
```

Expected:

- `init` succeeds even though daemon advertised `/home/me`.
- `run -- pwd` prints `/home/me/project`.
- remote operation log exists under `/home/me/project/.remork/log/operations.jsonl`.

- [ ] **Step 5: Remote sibling rejection smoke**

Run:

```bash
tmp_local="$(mktemp -d)"
cd "$tmp_local"
remork init remork-host-a:/home/me_other/not-allowed
```

Expected: fails with a clear message saying the workspace is outside advertised allowed roots.

- [ ] **Step 6: Update validation document**

Record command outputs, host, URL, daemon version, allowed root, child workspace, and cleanup actions in `docs/remork-product-v1-validation.md`.

---

## Product Decision

The current behavior should be treated as a product bug, not a user error. `remorkd --root /home/me` should mean "this daemon may serve workspaces under this directory". `remork init HOST:/home/me/project` should mean "bind this local folder to that concrete workspace". Requiring those two values to be identical makes one daemon per project the default, which conflicts with the intended product model.

## Self-Review

- Spec coverage: multi-workspace daemon semantics, durable install path, client-driven install, README clarity, API docs, Skill update, and remote validation are covered.
- Placeholder scan: no task uses TBD or defers unspecified error handling.
- Type consistency: shared package name is `internal/remoteroot`; the same containment semantics are used by daemon, init, and doctor.
