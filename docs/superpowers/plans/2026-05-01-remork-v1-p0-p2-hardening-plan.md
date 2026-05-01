# Remork V1 P0-P2 Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden Remork V1 across the accepted P0-P2 review findings: daemon-side path safety, apply consistency, network/resource bounds, accidental local artifact upload, local state isolation, shell/watch reliability, default-safety warnings, conflict recovery UX, and install workflow completeness.

**Architecture:** Keep the current Go monorepo and CLI/daemon split. Add small focused primitives around path-safe daemon writes, operation limits, ignore rules, per-checkout state identity, shell session lifecycle, and deploy execution. Preserve the V1 trust model of VPN/private-network deployment with optional bearer token, but make unsafe defaults visible.

**Tech Stack:** Go, Cobra CLI, net/http, gorilla/websocket, fsnotify, creack/pty, existing Remork packages under `internal/*`, Go tests and e2e tests.

---

## Scope And Priority

This plan covers P0 through P2 items accepted after five critical sub-agent reviews.

P0:

- Daemon-side `apply` must not write outside the configured remote root through symlink parents or symlink final paths.

P1:

- `apply` must report and reduce partial remote mutation risk.
- Client and daemon operations need timeouts, body limits, and output caps.
- `apply` must not broadly upload untracked local artifacts by default.
- Local state must be isolated per local checkout, not shared by every checkout of the same host and remote root.
- Shell sessions need a detach/reattach story so connection loss does not kill long-running interactive work.
- Watch needs periodic reconciliation and burst/reconnect coverage before users rely on it as a freshness guarantee.

P2:

- Unauthenticated non-loopback deployment remains allowed for trusted VPN V1, but must be a loud default-safety warning.
- Conflict recovery must be understandable from normal text output.
- `daemon install` and `upgrade` must be able to execute the generated `scp`/`ssh` plan, not only print it.

P3 operation-log hardening is intentionally not included here. Current per-workspace logs satisfy the V1 command-history requirement; tamper-resistant audit belongs to a separate trust-model plan.

## File Structure

Create:

- `internal/limits/limits.go`: shared defaults for HTTP timeout, daemon request limits, command output caps, and warning constants.
- `internal/ignore/ignore.go`: `.remorkignore` and simple `.gitignore` matcher used by local dirty detection.
- `internal/apply/lock.go`: per-root apply lock helper.
- `internal/apply/safe_path.go`: daemon mutation path resolver that rejects symlink parents and symlink final paths.
- `internal/cli/commands_conflict.go`: human-readable conflict inspection command.
- `internal/cli/daemon_runner.go`: injectable command runner for `daemon install --execute`.

Modify:

- `internal/apply/apply.go`: use safe mutation paths, lock applies, reverify before mutation, and return partial failure details.
- `internal/apply/apply_test.go`: symlink escape, lock, and partial failure regression tests.
- `internal/client/client.go`: add default timeout and bounded error body reads.
- `internal/client/client_test.go`: timeout/no-proxy/error-body tests.
- `internal/daemon/server.go`: cap apply body, pass output limit to exec, improve shell session lifecycle, and keep event handling compatible.
- `cmd/remorkd/main.go`: use `http.Server` timeouts and print non-loopback/no-token warnings.
- `internal/exec/exec.go`: cap stdout/stderr with truncation metadata.
- `internal/exec/exec_test.go`: output cap tests.
- `internal/state/state.go`: support ignore rules and distinguish untracked creates from tracked modifications.
- `internal/state/state_test.go`: `.remorkignore`, `.gitignore`, and ignored-create tests.
- `internal/syncer/apply.go`: build apply changesets with ignore/pathspec/untracked options.
- `internal/syncer/syncer.go`: carry status path details needed by text status and conflict UX.
- `internal/cli/commands_apply.go`: add pathspecs, `--include-untracked`, richer dry-run, and untracked refusal.
- `internal/cli/commands_status.go`: show top changed/conflict paths and `--verbose`.
- `internal/cli/commands_init.go`: derive state dir from host URL, remote root, and local root.
- `internal/cli/commands_daemon.go`: warnings, auth mode output, and `--execute` deploy flow.
- `internal/cli/commands_doctor.go`: expose auth/transport warnings in doctor.
- `internal/cli/commands_shell.go`: `--list`, `--attach`, `--kill`, disconnect messaging.
- `internal/cli/commands_watch.go`: periodic full reconcile, debounce window, and flags.
- `internal/pty/session.go`: keep sessions alive after websocket disconnect; list/attach/kill semantics.
- `internal/shellclient/shellclient.go`: attach existing session and surface detached session IDs.
- `README.md`: update safe deployment, ignore/apply, conflict, watch, shell, and install workflow docs.
- `docs/remork-api.md`: update changed API fields and limits.

Test:

- Existing unit tests under `internal/*`.
- Existing product e2e tests under `test/e2e`.
- Add new e2e tests in `test/e2e/remork_hardening_e2e_test.go`.

## Task 1: P0 Daemon Apply Symlink-Safe Mutation Paths

**Files:**

- Create: `internal/apply/safe_path.go`
- Modify: `internal/apply/apply.go`
- Modify: `internal/apply/apply_test.go`
- Test: `internal/apply/apply_test.go`

- [ ] **Step 1: Write failing symlink escape tests**

Append these tests to `internal/apply/apply_test.go`:

```go
func TestApplyRejectsCreateThroughSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}

	_, err := Apply(root, Changeset{Changes: []Change{
		{Path: "linked/escape.txt", Kind: ChangeCreate, Content: []byte("outside")},
	}})
	if err == nil {
		t.Fatal("Apply accepted create through symlink parent")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was touched: %v", err)
	}
}

func TestApplyRejectsUpdateOfSymlinkFile(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, []byte("outside-base"))
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	_, err := Apply(root, Changeset{Changes: []Change{
		{
			Path:     "link.txt",
			Kind:     ChangeUpdate,
			BaseHash: state.HashBytes([]byte("outside-base")),
			Content:  []byte("outside-after"),
		},
	}})
	if err == nil {
		t.Fatal("Apply accepted update of symlink file")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside-base" {
		t.Fatalf("outside file was modified: %q", data)
	}
}

func TestApplyRejectsDeleteOfSymlinkFile(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, []byte("outside-base"))
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	_, err := Apply(root, Changeset{Changes: []Change{
		{
			Path:     "link.txt",
			Kind:     ChangeDelete,
			BaseHash: state.HashBytes([]byte("outside-base")),
		},
	}})
	if err == nil {
		t.Fatal("Apply accepted delete of symlink file")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./internal/apply -run 'TestApplyRejects.*Symlink' -count=1 -v
```

Expected: FAIL. At least `TestApplyRejectsCreateThroughSymlinkParent` should show that the outside file was touched or no error was returned.

- [ ] **Step 3: Add safe daemon mutation resolver**

Create `internal/apply/safe_path.go`:

```go
package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"remork/internal/paths"
)

func resolveMutationPath(root, remotePath string) (string, error) {
	full, err := paths.ResolveInsideWorkspace(root, remotePath)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, full)
	if err != nil {
		return "", err
	}
	current := rootAbs
	parentRel := filepath.Dir(rel)
	if parentRel != "." {
		for _, part := range strings.Split(parentRel, string(filepath.Separator)) {
			if part == "" || part == "." {
				continue
			}
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("apply path %q uses symlink parent %q", remotePath, current)
			}
		}
	}
	if info, err := os.Lstat(full); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("apply path %q is a symlink", remotePath)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	fullRealParent := filepath.Dir(full)
	if _, err := os.Stat(fullRealParent); err == nil {
		parentReal, err := filepath.EvalSymlinks(fullRealParent)
		if err != nil {
			return "", err
		}
		if !insidePath(rootReal, parentReal) {
			return "", paths.ErrPathEscape
		}
	}
	return full, nil
}

func insidePath(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
```

- [ ] **Step 4: Use safe resolver in apply verification and mutation**

Modify `internal/apply/apply.go`:

```go
// In Apply, replace:
full, err := paths.ResolveInsideWorkspace(root, ch.Path)
// with:
full, err := resolveMutationPath(root, ch.Path)

// In verify, replace:
full, err := paths.ResolveInsideWorkspace(root, ch.Path)
// with:
full, err := resolveMutationPath(root, ch.Path)
```

Keep the `paths` import if another function still uses it. Remove the `paths` import if it becomes unused.

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/apply -run 'TestApplyRejects.*Symlink|TestApplyUpdateSucceedsWhenBaseMatches|TestApplyCreateAndDelete' -count=1 -v
```

Expected: PASS.

- [ ] **Step 6: Run package tests**

Run:

```bash
go test ./internal/apply ./internal/daemon ./test/e2e -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/apply/safe_path.go internal/apply/apply.go internal/apply/apply_test.go
git commit -m "fix: reject daemon apply symlink escapes"
```

## Task 2: P1 Network Timeouts, Body Limits, And Exec Output Caps

**Files:**

- Create: `internal/limits/limits.go`
- Modify: `internal/client/client.go`
- Modify: `internal/client/client_test.go`
- Modify: `internal/daemon/server.go`
- Modify: `cmd/remorkd/main.go`
- Modify: `internal/exec/exec.go`
- Modify: `internal/exec/exec_test.go`
- Test: `internal/client/client_test.go`, `internal/daemon/server_test.go`, `internal/exec/exec_test.go`

- [ ] **Step 1: Create shared limits**

Create `internal/limits/limits.go`:

```go
package limits

import "time"

const (
	MaxErrorBodyBytes = 64 << 10
	MaxApplyBodyBytes = 256 << 20
	MaxExecOutputBytes = 8 << 20
)

const (
	DefaultHTTPTimeout       = 30 * time.Second
	DaemonReadHeaderTimeout = 10 * time.Second
	DaemonReadTimeout       = 15 * time.Minute
	DaemonWriteTimeout      = 15 * time.Minute
	DaemonIdleTimeout       = 2 * time.Minute
)
```

- [ ] **Step 2: Write failing client timeout test**

Append to `internal/client/client_test.go`:

```go
func TestNewHTTPClientHasDefaultTimeout(t *testing.T) {
	c := NewHTTPClient(false)
	if c.Timeout != limits.DefaultHTTPTimeout {
		t.Fatalf("timeout = %s, want %s", c.Timeout, limits.DefaultHTTPTimeout)
	}
}

func TestNoProxyHTTPClientKeepsDefaultTimeout(t *testing.T) {
	c := NewHTTPClient(true)
	if c.Timeout != limits.DefaultHTTPTimeout {
		t.Fatalf("timeout = %s, want %s", c.Timeout, limits.DefaultHTTPTimeout)
	}
}
```

Add import:

```go
"remork/internal/limits"
```

- [ ] **Step 3: Run client test and verify failure**

Run:

```bash
go test ./internal/client -run 'Test.*DefaultTimeout' -count=1 -v
```

Expected: FAIL because `NewHTTPClient` currently returns `http.DefaultClient` or a client without `Timeout`.

- [ ] **Step 4: Implement default HTTP timeout and bounded error reads**

Modify `internal/client/client.go`:

```go
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"remork/internal/api"
	"remork/internal/apply"
	execx "remork/internal/exec"
	"remork/internal/limits"
	"remork/internal/ops"
)
```

Replace `NewHTTPClient` with:

```go
func NewHTTPClient(noProxy bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if noProxy {
		transport.Proxy = nil
	}
	return &http.Client{Transport: transport, Timeout: limits.DefaultHTTPTimeout}
}
```

Add helper:

```go
func readErrorBody(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, limits.MaxErrorBodyBytes))
	return string(data)
}
```

Replace every `body, _ := io.ReadAll(resp.Body)` error path with:

```go
body := readErrorBody(resp.Body)
```

- [ ] **Step 5: Write failing exec output cap tests**

Append to `internal/exec/exec_test.go`:

```go
func TestRunTruncatesLargeStdout(t *testing.T) {
	res, err := Run(Options{
		Command:        []string{"sh", "-c", "python3 - <<'PY'\nprint('x' * 9000000)\nPY"},
		MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.StdoutTruncated {
		t.Fatalf("stdout was not marked truncated")
	}
	if len(res.Stdout) > 1200 {
		t.Fatalf("stdout too large: %d", len(res.Stdout))
	}
}
```

- [ ] **Step 6: Run exec test and verify failure**

Run:

```bash
go test ./internal/exec -run TestRunTruncatesLargeStdout -count=1 -v
```

Expected: FAIL because `Options.MaxOutputBytes` and `Result.StdoutTruncated` do not exist.

- [ ] **Step 7: Implement bounded exec output**

Modify `internal/exec/exec.go`:

```go
type Options struct {
	Cwd            string
	Command        []string
	Env            []string
	Timeout        time.Duration
	MaxOutputBytes int64
}

type Result struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	TimedOut        bool   `json:"timed_out"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
}
```

Add this writer:

```go
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}
```

In `Run`, replace `var stdout, stderr bytes.Buffer` with:

```go
maxOutput := opts.MaxOutputBytes
if maxOutput == 0 {
	maxOutput = limits.MaxExecOutputBytes
}
stdout := &cappedBuffer{limit: maxOutput}
stderr := &cappedBuffer{limit: maxOutput}
```

Add import:

```go
"remork/internal/limits"
```

Build `Result` as:

```go
res := Result{
	Stdout:          stdout.String(),
	Stderr:          stderr.String(),
	StdoutTruncated: stdout.truncated,
	StderrTruncated: stderr.truncated,
}
```

- [ ] **Step 8: Cap apply request body and daemon server timeouts**

In `internal/daemon/server.go`, import `remork/internal/limits` and wrap the apply decoder in `handleApply`:

```go
r.Body = http.MaxBytesReader(w, r.Body, limits.MaxApplyBodyBytes)
var cs apply.Changeset
if err := json.NewDecoder(r.Body).Decode(&cs); err != nil {
	http.Error(w, err.Error(), http.StatusBadRequest)
	s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
	return
}
```

In `handleExec`, pass `MaxOutputBytes`:

```go
result, runErr := execx.Run(execx.Options{
	Cwd:            req.Cwd,
	Command:        req.Command,
	Timeout:        time.Duration(req.TimeoutMillis) * time.Millisecond,
	MaxOutputBytes: limits.MaxExecOutputBytes,
})
```

In `cmd/remorkd/main.go`, replace `http.ListenAndServe` with:

```go
httpServer := &http.Server{
	Addr:              *addr,
	Handler:           srv.Handler(),
	ReadHeaderTimeout: limits.DaemonReadHeaderTimeout,
	ReadTimeout:       limits.DaemonReadTimeout,
	WriteTimeout:      limits.DaemonWriteTimeout,
	IdleTimeout:       limits.DaemonIdleTimeout,
}
log.Fatal(httpServer.ListenAndServe())
```

Add import:

```go
"remork/internal/limits"
```

- [ ] **Step 9: Run focused tests**

Run:

```bash
go test ./internal/client ./internal/exec ./internal/daemon ./cmd/remorkd -count=1
```

Expected: PASS.

- [ ] **Step 10: Run broader tests**

Run:

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/limits internal/client internal/exec internal/daemon cmd/remorkd
git commit -m "fix: bound remork network and command resources"
```

## Task 3: P1 Prevent Broad Untracked Artifact Apply

**Files:**

- Create: `internal/ignore/ignore.go`
- Modify: `internal/state/state.go`
- Modify: `internal/state/state_test.go`
- Modify: `internal/syncer/apply.go`
- Modify: `internal/cli/commands_apply.go`
- Test: `internal/state/state_test.go`, `internal/syncer/syncer_test.go`, `test/e2e/remork_hardening_e2e_test.go`

- [ ] **Step 1: Write failing ignore tests**

Append to `internal/state/state_test.go`:

```go
func TestDetectDirtyRespectsRemorkIgnore(t *testing.T) {
	local := t.TempDir()
	mustWrite(t, filepath.Join(local, ".remorkignore"), []byte("node_modules/\n*.log\n.env\n"))
	mustWrite(t, filepath.Join(local, "node_modules", "pkg", "index.js"), []byte("ignored"))
	mustWrite(t, filepath.Join(local, "run.log"), []byte("ignored"))
	mustWrite(t, filepath.Join(local, ".env"), []byte("ignored"))
	mustWrite(t, filepath.Join(local, "src", "main.go"), []byte("tracked"))

	dirty, err := DetectDirtyWithOptions(local, Snapshot{Entries: map[string]TrackedFile{}}, DirtyOptions{
		UseIgnoreFiles: true,
	})
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	assertChange(t, dirty, "src/main.go", ChangeCreate)
	if hasChange(dirty, "node_modules/pkg/index.js") || hasChange(dirty, "run.log") || hasChange(dirty, ".env") {
		t.Fatalf("ignored paths were reported dirty: %#v", dirty)
	}
}

func hasChange(changes []DirtyChange, path string) bool {
	for _, change := range changes {
		if change.Path == path {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/state -run TestDetectDirtyRespectsRemorkIgnore -count=1 -v
```

Expected: FAIL because `DetectDirtyWithOptions` and `DirtyOptions` do not exist.

- [ ] **Step 3: Add ignore matcher**

Create `internal/ignore/ignore.go`:

```go
package ignore

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Rules struct {
	patterns []string
}

func Load(root string) Rules {
	var rules Rules
	for _, name := range []string{".remorkignore", ".gitignore"} {
		file, err := os.Open(filepath.Join(root, name))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			rules.patterns = append(rules.patterns, filepath.ToSlash(line))
		}
		_ = file.Close()
	}
	return rules
}

func (r Rules) Match(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	for _, pattern := range r.patterns {
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "/") {
			prefix := strings.TrimSuffix(pattern, "/")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
			continue
		}
		if strings.Contains(pattern, "*") {
			ok, _ := path.Match(pattern, path.Base(rel))
			if ok {
				return true
			}
			ok, _ = path.Match(pattern, rel)
			if ok {
				return true
			}
			continue
		}
		if rel == pattern || strings.HasPrefix(rel, pattern+"/") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add dirty options**

Modify `internal/state/state.go`:

```go
// add to the existing import block
"remork/internal/ignore"

type DirtyOptions struct {
	UseIgnoreFiles bool
}

func DetectDirty(localRoot string, snap Snapshot) ([]DirtyChange, error) {
	return DetectDirtyWithOptions(localRoot, snap, DirtyOptions{})
}

func DetectDirtyWithOptions(localRoot string, snap Snapshot, opts DirtyOptions) ([]DirtyChange, error) {
	// rename the current DetectDirty implementation to this function,
	// then insert the ignore checks below.
}
```

Inside the new `DetectDirtyWithOptions`, before `filepath.WalkDir`:

```go
rules := ignore.Rules{}
if opts.UseIgnoreFiles {
	rules = ignore.Load(localRoot)
}
```

Inside the walk, after `rel = filepath.ToSlash(rel)`:

```go
if opts.UseIgnoreFiles && rules.Match(rel) {
	return nil
}
```

For directories, after computing `rel`, skip ignored dirs:

```go
if d.IsDir() {
	if d.Name() == ".git" || d.Name() == ".remork" {
		return filepath.SkipDir
	}
	if opts.UseIgnoreFiles {
		rel, relErr := filepath.Rel(localRoot, path)
		if relErr == nil && rules.Match(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
	}
	return nil
}
```

- [ ] **Step 5: Run state tests**

Run:

```bash
go test ./internal/state -count=1
```

Expected: PASS.

- [ ] **Step 6: Make untracked creates explicit in apply**

Modify `internal/syncer/apply.go`:

```go
type BuildChangesetOptions struct {
	UseIgnoreFiles     bool
	IncludeUntracked   bool
	ExplicitPaths       []string
}

func BuildChangeset(localRoot string, snap state.Snapshot) (apply.Changeset, []SkippedChange, error) {
	return BuildChangesetWithOptions(localRoot, snap, BuildChangesetOptions{UseIgnoreFiles: true})
}

func BuildChangesetWithOptions(localRoot string, snap state.Snapshot, opts BuildChangesetOptions) (apply.Changeset, []SkippedChange, error) {
	dirty, err := state.DetectDirtyWithOptions(localRoot, snap, state.DirtyOptions{UseIgnoreFiles: opts.UseIgnoreFiles})
	if err != nil {
		return apply.Changeset{}, nil, err
	}
	skipped, err := skippedPlaceholderChanges(localRoot, snap)
	if err != nil {
		return apply.Changeset{}, nil, err
	}
	changes := make([]apply.Change, 0, len(dirty))
	selected := explicitPathSet(opts.ExplicitPaths)
}
```

Add helpers:

```go
func explicitPathSet(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, path := range paths {
		clean := strings.Trim(strings.TrimPrefix(filepath.ToSlash(path), "./"), "/")
		if clean != "" && clean != "." {
			out[clean] = true
		}
	}
	return out
}

func pathExplicitlySelected(path string, selected map[string]bool) bool {
	if len(selected) == 0 {
		return false
	}
	for prefix := range selected {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
```

Replace the existing `state.ChangeCreate` case with:

```go
case state.ChangeCreate:
	if !opts.IncludeUntracked && !pathExplicitlySelected(change.Path, selected) {
		skipped = append(skipped, SkippedChange{Path: change.Path, Reason: "untracked local file; pass --include-untracked or an explicit path"})
		continue
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return apply.Changeset{}, nil, err
	}
	changes = append(changes, apply.Change{Path: change.Path, Kind: apply.ChangeCreate, Content: data})
```

Add imports:

```go
"path/filepath"
```

- [ ] **Step 7: Update apply CLI pathspec and flags**

Modify `internal/cli/commands_apply.go`:

```go
var includeUntracked bool

cmd := &cobra.Command{
	Use:   "apply [path...]",
	Short: "Apply local changes to the remote workspace",
	Args:  cobra.ArbitraryArgs,
}

changeset, skipped, err := syncer.BuildChangesetWithOptions(localRoot, snap, syncer.BuildChangesetOptions{
	UseIgnoreFiles:   true,
	IncludeUntracked: includeUntracked,
	ExplicitPaths:     args,
})
```

Add flag:

```go
cmd.Flags().BoolVar(&includeUntracked, "include-untracked", false, "Allow untracked local files to be created on the remote")
```

Change no-op text when all changes are skipped:

```go
if len(changeset.Changes) == 0 && len(skipped) > 0 && !jsonOut {
	fmt.Fprintln(cmd.OutOrStdout(), "applied 0")
	fmt.Fprintln(cmd.ErrOrStderr(), "Skipped untracked or ignored files. Use remork apply <path> or --include-untracked when you intend to create remote files.")
	return nil
}
```

- [ ] **Step 8: Add CLI e2e for skipped untracked files**

Create `test/e2e/remork_hardening_e2e_test.go` with:

```go
package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplySkipsUntrackedFilesUnlessExplicit(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("tracked.txt", "base\n")
	h.bindAndSync()
	h.writeLocal("tracked.txt", "changed\n")
	h.writeLocal("local.log", "do-not-upload\n")

	out := h.runInLocal("apply")
	mustContain(t, out, "applied 1")
	h.assertRemote("tracked.txt", "changed\n")
	if _, err := os.Stat(filepath.Join(h.remote, "local.log")); !os.IsNotExist(err) {
		t.Fatal("untracked local.log was uploaded")
	}

	h.writeLocal("src/new.txt", "intentional\n")
	h.runInLocal("apply", "src/new.txt")
	h.assertRemote("src/new.txt", "intentional\n")
}
```

- [ ] **Step 9: Run tests**

Run:

```bash
go test ./internal/ignore ./internal/state ./internal/syncer ./internal/cli ./test/e2e -count=1
```

Expected: PASS.

- [ ] **Step 10: Update README apply section**

Modify `README.md` under "Applying safely":

```markdown
New local files are not created on the remote unless you explicitly select them.
Use `remork apply path/to/new-file` for a specific new file, or
`remork apply --include-untracked` when you intentionally want every untracked
local file that is not ignored.

Remork reads `.remorkignore` first and `.gitignore` second. Use `.remorkignore`
for local-only caches, secrets, generated outputs, virtual environments, and
agent scratch files that should never be applied.
```

- [ ] **Step 11: Commit**

```bash
git add internal/ignore internal/state internal/syncer internal/cli/commands_apply.go test/e2e/remork_hardening_e2e_test.go README.md
git commit -m "feat: require explicit untracked apply"
```

## Task 4: P1 Isolate State Per Local Checkout And Host URL

**Files:**

- Modify: `internal/cli/commands_init.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/cli/commands_workspace.go`
- Test: `internal/cli/root_test.go`

- [ ] **Step 1: Write failing state isolation test**

Append to `internal/cli/root_test.go`:

```go
func TestInitUsesDifferentStateDirForDifferentLocalRoots(t *testing.T) {
	home := t.TempDir()
	remote := t.TempDir()
	serverURL, cleanup := startTestDaemon(t, remote)
	defer cleanup()

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab": {Name: "lab", URL: serverURL},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	localA := filepath.Join(t.TempDir(), "a")
	localB := filepath.Join(t.TempDir(), "b")
	mustMkdir(t, localA)
	mustMkdir(t, localB)

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: localA})
	if _, err := executeCommand(cmd, "init", "lab:"+remote); err != nil {
		t.Fatalf("init A: %v", err)
	}
	cmd = NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: localB})
	if _, err := executeCommand(cmd, "init", "lab:"+remote); err != nil {
		t.Fatalf("init B: %v", err)
	}

	bindingA, _, err := workspace.ResolveFrom(localA)
	if err != nil {
		t.Fatal(err)
	}
	bindingB, _, err := workspace.ResolveFrom(localB)
	if err != nil {
		t.Fatal(err)
	}
	if bindingA.StateDir == bindingB.StateDir {
		t.Fatalf("state dirs shared: %s", bindingA.StateDir)
	}
}
```

If `startTestDaemon` or `mustMkdir` do not exist in `root_test.go`, add:

```go
func startTestDaemon(t *testing.T, remote string) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	return srv.URL, srv.Close
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
```

Add imports if missing:

```go
"net/http/httptest"

"remork/internal/daemon"
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/cli -run TestInitUsesDifferentStateDirForDifferentLocalRoots -count=1 -v
```

Expected: FAIL because `stableWorkspaceID` currently hashes only host and remote root.

- [ ] **Step 3: Include host URL and local root in workspace ID**

Modify `internal/cli/commands_init.go`:

```go
workspaceID := stableWorkspaceID(hostName, host.URL, remoteRoot, localRoot)
```

Replace `stableWorkspaceID` with:

```go
func stableWorkspaceID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "ws_" + hex.EncodeToString(sum[:])[:16]
}
```

Add import:

```go
"strings"
```

- [ ] **Step 4: Update existing tests for new signature**

Search:

```bash
rg "stableWorkspaceID" internal/cli
```

For each test call, replace:

```go
stableWorkspaceID("lab", "/remote/root")
```

with:

```go
stableWorkspaceID("lab", "http://127.0.0.1:17731", "/remote/root", "/tmp/local")
```

- [ ] **Step 5: Run CLI tests**

Run:

```bash
go test ./internal/cli -count=1
```

Expected: PASS.

- [ ] **Step 6: Update workspace output**

Modify `internal/cli/commands_workspace.go` to label the state as local-checkout scoped:

```go
fmt.Fprintf(out, "state_scope: local-checkout\n")
fmt.Fprintf(out, "state_dir: %s\n", binding.StateDir)
```

Update `internal/cli/commands_workspace_test.go` to assert:

```go
mustContain(t, out.String(), "state_scope: local-checkout")
```

- [ ] **Step 7: Run tests**

Run:

```bash
go test ./internal/cli ./internal/workspace ./test/e2e -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/commands_init.go internal/cli/commands_workspace.go internal/cli/*_test.go
git commit -m "fix: isolate state per local checkout"
```

## Task 5: P1 Apply Partial Failure Reporting And Apply Lock

**Files:**

- Create: `internal/apply/lock.go`
- Modify: `internal/apply/apply.go`
- Modify: `internal/apply/apply_test.go`
- Modify: `internal/cli/commands_apply.go`
- Modify: `docs/remork-api.md`
- Test: `internal/apply/apply_test.go`, `internal/cli`

- [ ] **Step 1: Write failing partial failure test**

Append to `internal/apply/apply_test.go`:

```go
func TestApplyReportsPartialFailure(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("a-base"))
	mustWrite(t, filepath.Join(root, "b.txt"), []byte("b-base"))

	oldRename := renameFile
	defer func() { renameFile = oldRename }()
	renameCount := 0
	renameFile = func(oldpath, newpath string) error {
		renameCount++
		if renameCount == 2 {
			return os.ErrPermission
		}
		return oldRename(oldpath, newpath)
	}

	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("a-base")), Content: []byte("a-after")},
		{Path: "b.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("b-base")), Content: []byte("b-after")},
	}})
	if err == nil {
		t.Fatal("expected partial failure")
	}
	if result.Applied {
		t.Fatal("partial failure must not report Applied=true")
	}
	if len(result.Partial) != 1 || result.Partial[0] != "a.txt" {
		t.Fatalf("partial paths = %#v", result.Partial)
	}
	if result.FailedPath != "b.txt" {
		t.Fatalf("failed path = %q", result.FailedPath)
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/apply -run TestApplyReportsPartialFailure -count=1 -v
```

Expected: FAIL because `renameFile`, `Result.Partial`, and `Result.FailedPath` do not exist.

- [ ] **Step 3: Add apply lock helper**

Create `internal/apply/lock.go`:

```go
package apply

import (
	"fmt"
	"os"
	"path/filepath"
)

type rootLock struct {
	path string
	file *os.File
}

func acquireRootLock(root string) (*rootLock, error) {
	lockDir := filepath.Join(root, ".remork", "lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(lockDir, "apply.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another apply is in progress for %s", root)
		}
		return nil, err
	}
	return &rootLock{path: path, file: file}, nil
}

func (l *rootLock) Release() error {
	if l == nil {
		return nil
	}
	_ = l.file.Close()
	return os.Remove(l.path)
}
```

- [ ] **Step 4: Add result fields and injectable filesystem operations**

Modify `internal/apply/apply.go`:

```go
type Result struct {
	Applied    bool     `json:"applied"`
	Conflicts  []string `json:"conflicts,omitempty"`
	Partial    []string `json:"partial,omitempty"`
	FailedPath string   `json:"failed_path,omitempty"`
}

var renameFile = os.Rename
var removeFile = os.Remove
```

Change mutation code to collect partial paths:

```go
lock, err := acquireRootLock(root)
if err != nil {
	return Result{Applied: false}, err
}
defer lock.Release()

if conflicts, err := verify(root, cs); err != nil {
	return Result{Applied: false}, err
} else if len(conflicts) > 0 {
	return Result{Applied: false, Conflicts: conflicts}, ErrConflict
}

var partial []string
for _, ch := range cs.Changes {
	full, err := resolveMutationPath(root, ch.Path)
	if err != nil {
		return Result{Applied: false, Partial: partial, FailedPath: ch.Path}, err
	}
	switch ch.Kind {
	case ChangeCreate, ChangeUpdate:
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return Result{Applied: false, Partial: partial, FailedPath: ch.Path}, err
		}
		tmp := full + ".remork-apply"
		if err := os.WriteFile(tmp, ch.Content, 0o644); err != nil {
			return Result{Applied: false, Partial: partial, FailedPath: ch.Path}, err
		}
		if err := renameFile(tmp, full); err != nil {
			_ = os.Remove(tmp)
			return Result{Applied: false, Partial: partial, FailedPath: ch.Path}, err
		}
		partial = append(partial, ch.Path)
	case ChangeDelete:
		if err := removeFile(full); err != nil {
			return Result{Applied: false, Partial: partial, FailedPath: ch.Path}, err
		}
		partial = append(partial, ch.Path)
	default:
		return Result{Applied: false, Partial: partial, FailedPath: ch.Path}, errors.New("unknown change kind")
	}
}
return Result{Applied: true}, nil
```

Keep the initial conflict verification before acquiring lock only if you immediately repeat verification after acquiring lock. The final code must verify after acquiring the lock as shown.

- [ ] **Step 5: Improve CLI partial failure output**

Modify `internal/cli/commands_apply.go` where `runner.ApplyChangeset` returns a non-conflict error:

```go
if len(result.Partial) > 0 || result.FailedPath != "" {
	if jsonOut {
		_ = output.WriteJSON(cmd.OutOrStdout(), result)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "apply failed at %s after remote paths were changed: %v\n", result.FailedPath, result.Partial)
	fmt.Fprintln(cmd.ErrOrStderr(), "Run remork status and remork sync before retrying.")
	return codedCommandError{code: exitcode.RemoteCommandFailed, err: err}
}
return err
```

- [ ] **Step 6: Run apply and CLI tests**

Run:

```bash
go test ./internal/apply ./internal/cli ./test/e2e -count=1
```

Expected: PASS.

- [ ] **Step 7: Update API docs**

Modify `docs/remork-api.md` in the apply result section:

```markdown
Apply results may include `partial` and `failed_path` when an unexpected remote
filesystem error happens after one or more paths were already changed. Conflicts
still reject the whole changeset before mutation, but Remork does not claim a
filesystem-level transaction for arbitrary multi-file failures.
```

- [ ] **Step 8: Commit**

```bash
git add internal/apply internal/cli/commands_apply.go docs/remork-api.md
git commit -m "fix: report partial apply failures"
```

## Task 6: P1 Watch Periodic Reconcile And Burst Coverage

**Files:**

- Modify: `internal/cli/commands_watch.go`
- Modify: `internal/cli/commands_watch_test.go`
- Modify: `test/e2e/remork_cli_e2e_test.go`

- [ ] **Step 1: Write failing watch reconcile unit test**

Append to `internal/cli/commands_watch_test.go`:

```go
func TestWatchSyncTargetReconcilesDeleteRenameAndOverflow(t *testing.T) {
	for _, ev := range []watch.Event{
		{Kind: watch.EventDelete, Path: "a.txt"},
		{Kind: watch.EventRename, Path: "a.txt"},
		{Kind: watch.EventOverflow, ResyncRequired: true},
	} {
		if got := watchSyncTarget(ev); got != "" {
			t.Fatalf("watchSyncTarget(%#v) = %q, want full reconcile", ev, got)
		}
	}
	if got := watchSyncTarget(watch.Event{Kind: watch.EventUpdate, Path: "src/main.txt"}); got != "src/main.txt" {
		t.Fatalf("update target = %q", got)
	}
}

func TestDefaultWatchOptions(t *testing.T) {
	opts := defaultWatchOptions()
	if opts.ReconcileInterval <= 0 {
		t.Fatalf("reconcile interval must be positive")
	}
	if opts.Debounce <= 0 {
		t.Fatalf("debounce must be positive")
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/cli -run 'TestWatchSyncTarget|TestDefaultWatchOptions' -count=1 -v
```

Expected: FAIL because `defaultWatchOptions` does not exist.

- [ ] **Step 3: Add watch options and flags**

Modify `internal/cli/commands_watch.go`:

```go
type watchOptions struct {
	ReconcileInterval time.Duration
	Debounce          time.Duration
}

func defaultWatchOptions() watchOptions {
	return watchOptions{ReconcileInterval: 30 * time.Second, Debounce: 200 * time.Millisecond}
}
```

In `addWatchCommand`:

```go
watchOpts := defaultWatchOptions()
cmd.Flags().DurationVar(&watchOpts.ReconcileInterval, "reconcile-interval", watchOpts.ReconcileInterval, "Full sync interval while watching")
cmd.Flags().DurationVar(&watchOpts.Debounce, "debounce", watchOpts.Debounce, "Delay to batch rapid watch events")

// inside RunE, call:
return watchEvents(ctx, cmd, runCtx, watchOpts)
```

Change signature:

```go
func watchEvents(ctx context.Context, cmd *cobra.Command, runCtx runContext, opts watchOptions) error
```

- [ ] **Step 4: Add periodic reconcile and debounce**

Inside `watchEvents`, create a ticker in the connected handler and event handler loop by refactoring event handling to:

```go
func handleWatchEvent(ctx context.Context, cmd *cobra.Command, runCtx runContext, lastRevision *string, ev watch.Event, debounce time.Duration) error {
	if debounce > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(debounce):
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", ev.Revision, ev.Kind, ev.Path)
	if _, err := syncForWatch(ctx, cmd, runCtx, watchSyncTarget(ev)); err != nil {
		return err
	}
	if needsWatchReconcile(ev) {
		manifest, err := runCtx.client.Manifest(runCtx.binding.RemoteRoot, ".")
		if err != nil {
			return err
		}
		*lastRevision = manifest.Revision
		fmt.Fprintf(cmd.OutOrStdout(), "reconciled %s\n", *lastRevision)
		return nil
	}
	if ev.Revision != "" {
		*lastRevision = ev.Revision
	}
	return nil
}
```

Add periodic full sync in `streamWorkspaceEvents` by accepting a `tick <-chan time.Time` parameter:

```go
func streamWorkspaceEvents(ctx context.Context, runCtx runContext, connected func() error, tick <-chan time.Time, handle func(watch.Event) error, reconcile func() error) error
```

In the read loop, start a goroutine reading JSON into an event channel, and select over event, tick, and ctx:

```go
events := make(chan watch.Event, 16)
readErr := make(chan error, 1)
go func() {
	for {
		var ev watch.Event
		if err := conn.ReadJSON(&ev); err != nil {
			readErr <- err
			return
		}
		events <- ev
	}
}()
for {
	select {
	case ev := <-events:
		if err := handle(ev); err != nil {
			return err
		}
	case <-tick:
		if reconcile != nil {
			if err := reconcile(); err != nil {
				return err
			}
		}
	case err := <-readErr:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

- [ ] **Step 5: Add e2e burst test**

Append to `test/e2e/remork_cli_e2e_test.go`:

```go
func TestRemorkProductWatchHandlesBurstRemoteEvents(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("burst/seed.txt", "seed\n")
	h.bindAndSync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	cmd := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: h.home, WorkingDir: h.local})
	cmd.SetContext(ctx)
	cmd.SetOut(writer)
	cmd.SetErr(writer)
	cmd.SetArgs([]string{"watch", "--debounce", "1ms", "--reconcile-interval", "50ms"})
	errCh := make(chan error, 1)
	go func() {
		err := cmd.Execute()
		_ = writer.Close()
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		errCh <- err
	}()
	lines := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForLine(t, lines, "watching")
	for i := 0; i < 25; i++ {
		h.writeRemote(fmt.Sprintf("burst/%02d.txt", i), fmt.Sprintf("%02d\n", i))
	}
	waitForLocalContent(t, filepath.Join(h.local, "burst", "24.txt"), "24\n")
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("watch returned error: %v", err)
	}
}
```

Add import:

```go
"fmt"
```

- [ ] **Step 6: Run watch tests**

Run:

```bash
go test ./internal/cli ./test/e2e -run 'Watch|TestRemorkProductWatch' -count=1 -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/commands_watch.go internal/cli/commands_watch_test.go test/e2e/remork_cli_e2e_test.go
git commit -m "feat: reconcile remork watch periodically"
```

## Task 7: P1 Durable Shell Sessions With Attach/List/Kill

**Files:**

- Modify: `internal/api/types.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/daemon/server_test.go`
- Modify: `internal/pty/session.go`
- Modify: `internal/shellclient/shellclient.go`
- Modify: `internal/cli/commands_shell.go`
- Test: `internal/daemon/server_test.go`, `test/e2e/remork_shell_e2e_test.go`

- [ ] **Step 1: Add API types**

Modify `internal/api/types.go`:

```go
type ShellSessionInfo struct {
	ID         string `json:"id"`
	Command    []string `json:"command"`
	LastActive string `json:"last_active"`
}
```

- [ ] **Step 2: Write failing daemon shell list test**

Append to `internal/daemon/server_test.go`:

```go
func TestShellSessionSurvivesClientDisconnectAndLists(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial shell: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("echo ready\n")); err != nil {
		t.Fatalf("write shell: %v", err)
	}
	_ = conn.Close()

	resp, err := http.Get(srv.URL + "/shell/sessions?root=" + url.QueryEscape(root))
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"id"`) {
		t.Fatalf("session not listed: %s", body)
	}
}
```

- [ ] **Step 3: Run test and verify failure**

Run:

```bash
go test ./internal/daemon -run TestShellSessionSurvivesClientDisconnectAndLists -count=1 -v
```

Expected: FAIL because `/shell/sessions` does not exist and shell currently closes sessions on disconnect.

- [ ] **Step 4: Add session attach/list/kill endpoints**

Modify `internal/daemon/server.go` in `NewServer`:

```go
s.mux.HandleFunc("/shell/sessions", s.withAuth(s.handleShellSessions))
```

Add handler:

```go
func (s *Server) handleShellSessions(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		sessions := s.ptyManager.List()
		out := make([]api.ShellSessionInfo, 0, len(sessions))
		for _, session := range sessions {
			out = append(out, api.ShellSessionInfo{
				ID:         session.ID,
				Command:    session.Command,
				LastActive: session.LastActive.UTC().Format(time.RFC3339Nano),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": out})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}
		_ = s.ptyManager.Close(id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
```

Modify `handleShell` to accept `session` query:

```go
sessionID := r.URL.Query().Get("session")
var session *ptysession.Session
if sessionID != "" {
	session = s.ptyManager.Get(sessionID)
	if session == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		s.finishOperation(op, http.StatusNotFound, "error", "session not found")
		return
	}
	} else {
		session, err = s.ptyManager.Start(ptysession.StartOptions{Command: []string{"sh"}, Cwd: root, Rows: 24, Cols: 80})
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
			s.finishOperation(op, http.StatusInternalServerError, "error", err.Error())
			return
		}
	}
```

Remove:

```go
defer s.ptyManager.CloseSession(session)
```

When the shell exits normally, close the session after `done`:

```go
case status := <-done:
	applyShellStatus(status)
	_ = s.ptyManager.CloseSession(session)
	return
```

- [ ] **Step 5: Add pty manager Get**

Modify `internal/pty/session.go`:

```go
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}
```

- [ ] **Step 6: Update shell client for attach**

Modify `internal/shellclient/shellclient.go`:

```go
type Options struct {
	BaseURL   string
	Root      string
	ClientID  string
	Token     string
	NoProxy   bool
	Stdin     io.Reader
	Stdout    io.Writer
	Rows      int
	Cols      int
	Dialer    *websocket.Dialer
	SessionID string
}
```

Modify `BuildURL`:

```go
func BuildURL(baseURL, root string, sessionID ...string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/shell"
	q := u.Query()
	q.Set("root", root)
	if len(sessionID) > 0 && sessionID[0] != "" {
		q.Set("session", sessionID[0])
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
```

Call it from `Run`:

```go
wsURL, err := BuildURL(opts.BaseURL, opts.Root, opts.SessionID)
```

- [ ] **Step 7: Update CLI shell modes**

Modify `internal/cli/commands_shell.go`:

```go
var attachID string
var list bool
var killID string

cmd.Flags().StringVar(&attachID, "attach", "", "Attach to an existing remote shell session")
cmd.Flags().BoolVar(&list, "list", false, "List remote shell sessions")
cmd.Flags().StringVar(&killID, "kill", "", "Kill a remote shell session")
```

At start of `RunE`, after `newRunContext`:

```go
if list {
	return printShellSessions(cmd, runCtx)
}
if killID != "" {
	return killShellSession(cmd, runCtx, killID)
}
```

Pass attach:

```go
SessionID: attachID,
```

Add the helper functions in `internal/cli/commands_shell.go`:

```go
func printShellSessions(cmd *cobra.Command, runCtx runContext) error {
	url := strings.TrimRight(runCtx.host.URL, "/") + "/shell/sessions?root=" + neturl.QueryEscape(runCtx.binding.RemoteRoot)
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	runCtx.addAuth(req)
	resp, err := runCtx.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list shell sessions: %s", resp.Status)
	}
	var payload struct {
		Sessions []api.ShellSessionInfo `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if len(payload.Sessions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no shell sessions")
		return nil
	}
	for _, session := range payload.Sessions {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", session.ID, session.LastActive, strings.Join(session.Command, " "))
	}
	return nil
}

func killShellSession(cmd *cobra.Command, runCtx runContext, id string) error {
	url := strings.TrimRight(runCtx.host.URL, "/") + "/shell/sessions?root=" + neturl.QueryEscape(runCtx.binding.RemoteRoot) + "&id=" + neturl.QueryEscape(id)
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	runCtx.addAuth(req)
	resp, err := runCtx.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("kill shell session: %s", resp.Status)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "killed %s\n", id)
	return nil
}
```

Add imports:

```go
"encoding/json"
"net/http"
neturl "net/url"

"remork/internal/api"
```

After shell client returns with socket close and no exit frame, print:

```go
fmt.Fprintln(cmd.ErrOrStderr(), "Shell disconnected. If the remote session is still running, use remork shell --list and remork shell --attach <id>.")
```

- [ ] **Step 8: Run shell tests**

Run:

```bash
go test ./internal/pty ./internal/shellclient ./internal/daemon ./test/e2e -run 'Shell|Pty|Session' -count=1
```

Expected: PASS.

- [ ] **Step 9: Update README shell section**

Add:

```markdown
If the network disconnects, the daemon keeps the remote PTY session until it
exits or is reaped as idle. Use:

remork shell --list
remork shell --attach <session-id>
remork shell --kill <session-id>
```

- [ ] **Step 10: Commit**

```bash
git add internal/api internal/daemon internal/pty internal/shellclient internal/cli/commands_shell.go test/e2e/remork_shell_e2e_test.go README.md
git commit -m "feat: support durable shell sessions"
```

## Task 8: P2 Default-Safety Warnings For Non-Loopback No-Token Deployment

**Files:**

- Modify: `cmd/remorkd/main.go`
- Modify: `internal/cli/commands_daemon.go`
- Modify: `internal/cli/commands_doctor.go`
- Modify: `internal/cli/commands_daemon_test.go`
- Modify: `README.md`

- [ ] **Step 1: Add helper tests for unsafe address detection**

Create `internal/cli/commands_daemon_test.go` if it does not exist, or append:

```go
func TestDaemonDeployPlanWarnsForNonLoopbackWithoutToken(t *testing.T) {
	var out bytes.Buffer
	printDaemonDeployPlan(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "0.0.0.0:17731",
		remoteBin: "/tmp/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
	})
	got := out.String()
	mustContain(t, got, "WARNING")
	mustContain(t, got, "remote command execution")
	mustContain(t, got, "--token-file")
}
```

Add imports:

```go
"bytes"
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/cli -run TestDaemonDeployPlanWarnsForNonLoopbackWithoutToken -count=1 -v
```

Expected: FAIL because no warning is printed.

- [ ] **Step 3: Add address helpers and deploy warning**

Modify `internal/cli/commands_daemon.go`:

```go
func insecureNonLoopback(addr, tokenFile string) bool {
	if tokenFile != "" {
		return false
	}
	host := addr
	if strings.Contains(addr, ":") {
		parts := strings.Split(addr, ":")
		host = strings.Join(parts[:len(parts)-1], ":")
	}
	host = strings.Trim(host, "[]")
	return host == "0.0.0.0" || host == "::" || host == ""
}
```

At the top of `printDaemonDeployPlan`, after the intro:

```go
if insecureNonLoopback(deploy.addr, deploy.tokenFile) {
	fmt.Fprintln(out, "WARNING: this plan starts remorkd on a non-loopback address without a token.")
	fmt.Fprintln(out, "Anyone who can reach the port can use file apply, remote command execution, and shell endpoints for the configured root.")
	fmt.Fprintln(out, "For shared VPNs or lab networks, pass --token-file /path/to/remork.token and configure remork host add --token-env.")
	fmt.Fprintln(out)
}
```

- [ ] **Step 4: Add daemon startup warning**

Modify `cmd/remorkd/main.go`:

```go
if warnsInsecureBind(*addr, resolvedToken) {
	log.Printf("WARNING: remorkd is listening on %s without a token; this exposes apply, exec, and shell to anyone who can reach the port", *addr)
}
```

Add helper:

```go
func warnsInsecureBind(addr, token string) bool {
	if token != "" {
		return false
	}
	host := addr
	if strings.Contains(addr, ":") {
		parts := strings.Split(addr, ":")
		host = strings.Join(parts[:len(parts)-1], ":")
	}
	host = strings.Trim(host, "[]")
	return host == "0.0.0.0" || host == "::" || host == ""
}
```

- [ ] **Step 5: Surface auth mode in doctor**

Modify `internal/cli/commands_doctor.go` output to include:

```go
if host.TokenEnv == "" {
	fmt.Fprintln(cmd.OutOrStdout(), "WARN: host has no token configured; use only on trusted VPN/private networks")
}
```

Place it after host config is loaded.

- [ ] **Step 6: Update README**

Next to every `--addr 0.0.0.0:...` example, add:

```markdown
This exposes Remork to every machine that can reach the VPN/private address.
Use `--token-file` on shared VPNs or any multi-user network.
```

- [ ] **Step 7: Run tests**

Run:

```bash
go test ./cmd/remorkd ./internal/cli -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/remorkd internal/cli README.md
git commit -m "docs: warn on unauthenticated non-loopback daemon"
```

## Task 9: P2 Conflict Recovery UX

**Files:**

- Create: `internal/cli/commands_conflict.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/commands_status.go`
- Modify: `internal/cli/commands_apply.go`
- Modify: `test/e2e/remork_conflict_e2e_test.go`
- Modify: `README.md`

- [ ] **Step 1: Write failing status path test**

Append to `test/e2e/remork_conflict_e2e_test.go`:

```go
func TestStatusShowsConflictPathsAndNextSteps(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "base\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "local\n")
	h.writeRemote("a.txt", "remote\n")

	out := h.runInLocal("status")
	mustContain(t, out, "Conflicts: 1")
	mustContain(t, out, "a.txt")
	mustContain(t, out, "remork conflict a.txt")
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./test/e2e -run TestStatusShowsConflictPathsAndNextSteps -count=1 -v
```

Expected: FAIL because text status does not list conflict paths.

- [ ] **Step 3: Print path details in text status**

Modify `internal/cli/commands_status.go`:

```go
var verbose bool

cmd.Flags().BoolVar(&verbose, "verbose", false, "Show changed and conflict paths")
```

After counts:

```go
if status.Conflicts > 0 {
	fmt.Fprintln(cmd.OutOrStdout(), "Conflict paths:")
	for _, path := range limitedPaths(status.ConflictPaths, verbose) {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", path)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Next: remork conflict <path> or remork restore <path> after reviewing remote changes")
} else if status.LocalChanges > 0 && verbose {
	fmt.Fprintln(cmd.OutOrStdout(), "Changed paths:")
	for _, path := range limitedPaths(status.ChangedPaths, true) {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", path)
	}
}
```

Add helper:

```go
func limitedPaths(paths []string, verbose bool) []string {
	if verbose || len(paths) <= 10 {
		return paths
	}
	return paths[:10]
}
```

- [ ] **Step 4: Add conflict command**

Create `internal/cli/commands_conflict.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func addConflictCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "conflict PATH",
		Short: "Show recovery steps for a conflict path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			fmt.Fprintf(cmd.OutOrStdout(), "Conflict: %s\n", path)
			fmt.Fprintln(cmd.OutOrStdout(), "Review local changes:")
			fmt.Fprintf(cmd.OutOrStdout(), "  remork diff %s\n", path)
			fmt.Fprintln(cmd.OutOrStdout(), "Keep remote version locally:")
			fmt.Fprintf(cmd.OutOrStdout(), "  remork restore %s\n", path)
			fmt.Fprintln(cmd.OutOrStdout(), "After resolving, run:")
			fmt.Fprintln(cmd.OutOrStdout(), "  remork status")
			fmt.Fprintln(cmd.OutOrStdout(), "  remork apply")
			return nil
		},
	}
	root.AddCommand(cmd)
}
```

Modify `internal/cli/root.go`:

```go
addConflictCommand(root, opts)
```

Add to help command categories:

```go
"conflict": "Show recovery steps for a conflict path",
```

- [ ] **Step 5: Improve apply conflict output**

Modify `writeApplyConflict` in `internal/cli/commands_apply.go`:

```go
for _, path := range paths {
	fmt.Fprintf(cmd.ErrOrStderr(), "conflict: %s\n", path)
	fmt.Fprintf(cmd.ErrOrStderr(), "  inspect: remork conflict %s\n", path)
}
```

- [ ] **Step 6: Run conflict tests**

Run:

```bash
go test ./internal/cli ./test/e2e -run 'Conflict|Status' -count=1
```

Expected: PASS.

- [ ] **Step 7: Update README**

Add a "Conflict recovery" subsection:

```markdown
When `status` shows conflicts, start with:

remork status --verbose
remork conflict path/to/file

Use `remork diff path/to/file` to inspect local edits. Use `remork restore
path/to/file` only when you want the remote/base version locally. After resolving
local content, run `remork apply` again.
```

- [ ] **Step 8: Commit**

```bash
git add internal/cli README.md test/e2e/remork_conflict_e2e_test.go
git commit -m "feat: improve conflict recovery UX"
```

## Task 10: P2 Executable Daemon Install And Upgrade

**Files:**

- Create: `internal/cli/daemon_runner.go`
- Modify: `internal/cli/commands_daemon.go`
- Modify: `internal/cli/commands_daemon_test.go`
- Modify: `README.md`

- [ ] **Step 1: Write failing execute test**

Append to `internal/cli/commands_daemon_test.go`:

```go
func TestDaemonInstallExecuteRunsPlanCommands(t *testing.T) {
	var ran []string
	runner := commandRunnerFunc(func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	})
	var out bytes.Buffer
	deploy := daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: "/tmp/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		runner:    runner,
	}
	if err := runDaemonDeploy(&out, deploy); err != nil {
		t.Fatalf("run deploy: %v", err)
	}
	if len(ran) != 3 {
		t.Fatalf("commands = %#v", ran)
	}
	if !strings.HasPrefix(ran[0], "scp ") || !strings.HasPrefix(ran[1], "ssh ") || !strings.HasPrefix(ran[2], "ssh ") {
		t.Fatalf("unexpected commands: %#v", ran)
	}
}
```

Add imports:

```go
"strings"
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/cli -run TestDaemonInstallExecuteRunsPlanCommands -count=1 -v
```

Expected: FAIL because `runDaemonDeploy`, `commandRunnerFunc`, and execute options do not exist.

- [ ] **Step 3: Add command runner abstraction**

Create `internal/cli/daemon_runner.go`:

```go
package cli

import (
	"os"
	"os/exec"
)

type commandRunner interface {
	Run(name string, args ...string) error
}

type osCommandRunner struct{}

func (osCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

type commandRunnerFunc func(name string, args ...string) error

func (f commandRunnerFunc) Run(name string, args ...string) error {
	return f(name, args...)
}
```

- [ ] **Step 4: Add execute fields and deploy runner**

Modify `daemonDeployOptions` in `internal/cli/commands_daemon.go`:

```go
execute bool
yes     bool
runner  commandRunner
```

Add flags:

```go
cmd.Flags().BoolVar(&deploy.execute, "execute", false, "Run the generated scp/ssh commands")
cmd.Flags().BoolVar(&deploy.yes, "yes", false, "Confirm execution without prompting")
```

In `RunE`, replace `printDaemonDeployPlan` with:

```go
return runDaemonDeploy(cmd.OutOrStdout(), deploy)
```

Add:

```go
func runDaemonDeploy(out interface{ Write([]byte) (int, error) }, deploy daemonDeployOptions) error {
	printDaemonDeployPlan(out, deploy)
	if !deploy.execute {
		return nil
	}
	if !deploy.yes {
		return fmt.Errorf("--execute requires --yes")
	}
	if deploy.runner == nil {
		deploy.runner = osCommandRunner{}
	}
	remote := deploy.sshTarget
	if remote == "" {
		remote = deploy.hostName
	}
	if err := deploy.runner.Run("scp", deploy.localBin, remote+":"+deploy.remoteBin); err != nil {
		return err
	}
	if err := deploy.runner.Run("ssh", remote, "chmod 0755 "+deploy.remoteBin); err != nil {
		return err
	}
	startCmd := remoteStartCommand(deploy)
	if startCmd != "" {
		if err := deploy.runner.Run("ssh", remote, startCmd); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "daemon deploy executed")
	return nil
}
```

- [ ] **Step 5: Run daemon CLI tests**

Run:

```bash
go test ./internal/cli -run 'Daemon|Install|Upgrade' -count=1
```

Expected: PASS.

- [ ] **Step 6: Update README install section**

Add:

```markdown
`remork daemon install` prints the exact copy/start commands by default. To let
Remork run those commands through `scp` and `ssh`, add:

remork daemon install lab-a --root /data/project-a --ssh lab-a --platform linux-arm64 --execute --yes

The runtime transport is still HTTP to `remorkd`; SSH is only an install helper.
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/commands_daemon.go internal/cli/commands_daemon_test.go internal/cli/daemon_runner.go README.md
git commit -m "feat: execute daemon deploy plans"
```

## Task 11: Final Integration Verification

**Files:**

- Modify: `README.md`
- Modify: `docs/remork-api.md`
- Modify: `docs/remork-v1-10x-reliability.md`

- [ ] **Step 1: Run full unit and e2e suite**

Run:

```bash
go test -count=1 ./...
```

Expected: PASS.

- [ ] **Step 2: Run race suite**

Run:

```bash
go test -race -count=1 ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./internal/shellclient ./internal/watch ./test/e2e
```

Expected: PASS.

- [ ] **Step 3: Build release artifacts**

Run:

```bash
scripts/build-release.sh dev
```

Expected: creates or refreshes all `dist/remork*` and `dist/remorkd*` binaries plus `dist/checksums.txt`.

- [ ] **Step 4: Verify release checksums**

Run:

```bash
(cd dist && shasum -a 256 -c checksums.txt)
```

Expected: every artifact reports `OK`.

- [ ] **Step 5: Run targeted remote smoke on both provided hosts**

Run the existing remote smoke pattern from `docs/remork-v1-10x-reliability.md` against:

```text
z00879328_docker
z00879328_docker_2.6
```

Expected:

```text
z00879328_docker final remote smoke PASS
z00879328_docker_2.6 final remote smoke PASS
```

Cleanup commands must return no output:

```bash
ssh z00879328_docker 'rm -rf /tmp/remork-v1-hardening-*; ps -ef | grep remork-v1-hardening | grep -v grep || true; find /tmp -maxdepth 1 -name "remork-v1-hardening-*" -print 2>/dev/null | sort'
ssh z00879328_docker_2.6 'rm -rf /tmp/remork-v1-hardening-*; ps -ef | grep remork-v1-hardening | grep -v grep || true; find /tmp -maxdepth 1 -name "remork-v1-hardening-*" -print 2>/dev/null | sort'
```

- [ ] **Step 6: Update reliability report**

Append a section to `docs/remork-v1-10x-reliability.md`:

```markdown
## P0-P2 Hardening Verification

Date: 2026-05-01

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

Commands:

```text
go test -count=1 ./...
go test -race -count=1 ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./internal/shellclient ./internal/watch ./test/e2e
scripts/build-release.sh dev
(cd dist && shasum -a 256 -c checksums.txt)
```
```

- [ ] **Step 7: Run diff check**

Run:

```bash
git diff --check
```

Expected: no output, exit code 0.

- [ ] **Step 8: Commit final docs and verification**

```bash
git add README.md docs/remork-api.md docs/remork-v1-10x-reliability.md
git commit -m "docs: record remork hardening verification"
```

## Self-Review

Spec coverage:

- P0 daemon apply symlink escape: Task 1.
- P1 partial apply consistency: Task 5.
- P1 network timeout/body/output limits: Task 2.
- P1 broad untracked local apply: Task 3.
- P1 shared state across checkouts and host retargeting: Task 4.
- P1 shell disconnect fragility: Task 7.
- P1/P2 watch freshness reliability: Task 6.
- P2 VPN-only default-safety warnings: Task 8.
- P2 conflict recovery UX: Task 9.
- P2 install workflow completeness: Task 10.
- Final verification, remote smoke, docs: Task 11.

Placeholder scan:

- No placeholder markers or unspecified test steps remain.
- Every implementation task includes concrete files, test names, commands, expected results, and commit commands.

Type consistency:

- `limits.MaxExecOutputBytes`, `limits.DefaultHTTPTimeout`, and `limits.MaxApplyBodyBytes` are defined in Task 2 before use.
- `DirtyOptions`, `BuildChangesetOptions`, and `DetectDirtyWithOptions` are defined in Task 3 before use.
- `Result.Partial` and `Result.FailedPath` are defined in Task 5 before CLI usage.
- `watchOptions` and `defaultWatchOptions` are defined in Task 6 before flag wiring.
- `ShellSessionInfo`, `SessionID`, and session endpoints are defined in Task 7 before CLI usage.
- `daemonDeployOptions.execute`, `daemonDeployOptions.yes`, and `commandRunner` are defined in Task 10 before test usage.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-01-remork-v1-p0-p2-hardening-plan.md`. Two execution options:

1. Subagent-Driven (recommended) - dispatch a fresh subagent per task, review between tasks, fast iteration.
2. Inline Execution - execute tasks in this session using executing-plans, batch execution with checkpoints.

Recommended: Subagent-Driven, because the tasks touch mostly independent subsystems and each task has a narrow file set.
