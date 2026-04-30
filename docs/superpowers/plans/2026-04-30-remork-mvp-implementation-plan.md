# Remork MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a TDD-first MVP of `remork`: a local CLI plus remote daemon for editable local working copies, explicit apply, sync/pull, safe exec, PTY shell, and watch/events.

**Architecture:** Implement a Go monorepo with two binaries: `remork` for the local CLI and `remorkd` for the remote daemon. Go is a build-time dependency only: remote servers receive prebuilt `remorkd` binaries and do not need Go or internet access. Core behavior lives in focused internal packages so the sync planner, manifest scanner, apply checker, state store, and transport client can be tested without running full end-to-end processes.

**Tech Stack:** Go 1.22+ on the local build machine or CI, stdlib `net/http`, Cobra CLI, Gorilla WebSocket, fsnotify, creack/pty, JSON state store with atomic writes, Go unit/integration tests, cross-compiled release binaries for offline remote servers.

---

## Scope Check

The approved spec covers several subsystems: daemon, CLI, sync/pull, local state, apply, exec, PTY shell, and watch/events. This plan keeps them in one MVP because each subsystem is needed to validate the core workflow, but tasks are ordered as vertical slices with explicit test boundaries. Each task must end with tests passing and a commit.

## Remote Validation Targets

Use these real hosts for final offline-daemon validation after local tests pass:

```text
Alias: z00879328_docker
SSH: root@175.100.2.7:22022
Platform probe: Linux aarch64
Release artifact: dist/remorkd-linux-arm64
Notes: SSH config includes ControlMaster and RemoteForward for proxy, but remork daemon transport must still be tested through direct VPN HTTP, not SSH tunnel.

Alias: z00879328_docker_2.6
SSH: root@175.100.2.6:2226
Platform probe: Linux aarch64
Release artifact: dist/remorkd-linux-arm64
Notes: Minimal environment; `hostname` was not available during probe. Do not assume common convenience tools beyond POSIX shell basics.
```

Remote validation must not run `go build`, `go get`, `brew`, `apt`, `npm`, or any internet-dependent setup on these hosts. The only expected remote install action is copying a prebuilt `remorkd-linux-arm64` binary and a small test workspace under `/tmp`.

## Repository Structure

Create this structure:

```text
go.mod
go.sum
.gitignore
scripts/build-release.sh
deploy/remorkd.example.toml
cmd/remork/main.go
cmd/remorkd/main.go
internal/api/types.go
internal/apply/apply.go
internal/apply/apply_test.go
internal/client/client.go
internal/client/client_test.go
internal/config/config.go
internal/config/config_test.go
internal/daemon/server.go
internal/daemon/server_test.go
internal/exec/exec.go
internal/exec/exec_test.go
internal/manifest/manifest.go
internal/manifest/manifest_test.go
internal/paths/paths.go
internal/paths/paths_test.go
internal/planner/planner.go
internal/planner/planner_test.go
internal/progress/progress.go
internal/progress/progress_test.go
internal/pty/session.go
internal/pty/session_test.go
internal/state/state.go
internal/state/state_test.go
internal/transfer/transfer.go
internal/transfer/transfer_test.go
internal/watch/watch.go
internal/watch/watch_test.go
test/e2e/remork_e2e_test.go
```

Responsibilities:

- `internal/api`: shared request/response structs used by daemon, client, and tests.
- `internal/paths`: remote path normalization and workspace escape prevention.
- `internal/manifest`: filesystem scanning, file classification, hash policy, and `.meta` payloads.
- `internal/state`: local base state, dirty tracking, and atomic persistence under `~/.remork/state`.
- `internal/planner`: sync/pull/apply planning from manifest plus local state.
- `internal/transfer`: range/chunk transfer helpers and resumable file writes.
- `internal/apply`: changeset construction and daemon-side base verification.
- `internal/daemon`: HTTP/WebSocket server wiring.
- `internal/client`: HTTP/WebSocket client for CLI operations.
- `internal/exec`: non-interactive command execution and safe-mode preflight.
- `internal/pty`: interactive shell session lifecycle.
- `internal/watch`: filesystem watch event normalization and reconciliation signals.
- `internal/config`: host/workspace config file handling.
- `internal/progress`: progress reporter abstraction for interactive and quiet modes.
- `scripts/build-release.sh`: builds local and cross-compiled release artifacts.
- `deploy/remorkd.example.toml`: minimal offline daemon config template copied next to `remorkd`.

## Task 0: Toolchain Bootstrap And Module Skeleton

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `scripts/build-release.sh`
- Create: `deploy/remorkd.example.toml`
- Create: `cmd/remork/main.go`
- Create: `cmd/remorkd/main.go`
- Create: `internal/api/types.go`

- [ ] **Step 1: Verify local Go toolchain is unavailable or usable**

Run:

```bash
go version
```

Expected on the current machine before bootstrap:

```text
zsh:1: command not found: go
```

If Go is already installed on the local execution machine or CI, continue without installing. Do not install Go on remote servers; remote servers receive prebuilt `remorkd` binaries.

- [ ] **Step 2: Install Go locally when missing**

Run:

```bash
brew install go
```

Expected:

```text
go was successfully installed
```

Then verify:

```bash
go version
```

Expected: Go 1.22 or newer.

- [ ] **Step 3: Create module file**

Create `go.mod`:

```go
module remork

go 1.22
```

- [ ] **Step 4: Create gitignore for generated artifacts**

Create `.gitignore`:

```gitignore
dist/
remork
remorkd
*.remork-tmp
*.remork-apply
```

- [ ] **Step 5: Create shared API types**

Create `internal/api/types.go`:

```go
package api

type FileType string

const (
	FileTypeFile    FileType = "file"
	FileTypeDir     FileType = "directory"
	FileTypeSymlink FileType = "symlink"
	FileTypeSpecial FileType = "special"
)

type FileEntry struct {
	Path        string   `json:"path"`
	Type        FileType `json:"type"`
	Size        int64    `json:"size"`
	ModTimeUnix int64    `json:"mtime"`
	Hash        string   `json:"hash,omitempty"`
	Revision    string   `json:"revision"`
	Large       bool     `json:"large"`
	Error       string   `json:"error,omitempty"`
}

type ManifestResponse struct {
	Root      string      `json:"root"`
	Path      string      `json:"path"`
	Revision  string      `json:"revision"`
	Entries   []FileEntry `json:"entries"`
	Threshold int64       `json:"threshold"`
}

type LargeFileMeta struct {
	Kind        string `json:"kind"`
	RemotePath  string `json:"remote_path"`
	Size        int64  `json:"size"`
	ModTimeUnix int64  `json:"mtime"`
	Hash        string `json:"hash,omitempty"`
	Revision    string `json:"revision"`
	Pulled      bool   `json:"pulled"`
	PullCommand string `json:"pull_command"`
}

type StatusResponse struct {
	Version       string   `json:"version"`
	Roots         []string `json:"roots"`
	Threshold     int64    `json:"threshold"`
	Platform      string   `json:"platform"`
	WatchSupported bool    `json:"watch_supported"`
}
```

- [ ] **Step 6: Create stub binaries**

Create `cmd/remork/main.go`:

```go
package main

import (
	"flag"
	"fmt"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("remork " + version)
		return
	}
	fmt.Println("remork")
}
```

Create `cmd/remorkd/main.go`:

```go
package main

import (
	"flag"
	"fmt"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("remorkd " + version)
		return
	}
	fmt.Println("remorkd")
}
```

- [ ] **Step 7: Add offline daemon config template**

Create `deploy/remorkd.example.toml`:

```toml
listen_addr = "0.0.0.0:7731"
workspace_roots = ["/data/project"]
large_file_threshold = "128MB"

# Optional deployment guards for VPN environments:
# allowlist = ["10.0.0.0/8"]
# token = ""
```

- [ ] **Step 8: Add release build script**

Create `scripts/build-release.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

version="${1:-dev}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$repo_root/dist"
mkdir -p "$dist_dir"
rm -f "$dist_dir"/remork "$dist_dir"/remorkd-* "$dist_dir"/checksums.txt

build_daemon() {
  local goos="$1"
  local goarch="$2"
  local out="$dist_dir/remorkd-$goos-$goarch"
  echo "building $out"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.version=$version" \
    -o "$out" ./cmd/remorkd
}

go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$dist_dir/remork" ./cmd/remork
build_daemon linux amd64
build_daemon linux arm64
build_daemon darwin amd64
build_daemon darwin arm64

cp "$repo_root/deploy/remorkd.example.toml" "$dist_dir/remorkd.example.toml"
(
  cd "$dist_dir"
  shasum -a 256 remork remorkd-* remorkd.example.toml > checksums.txt
)
```

- [ ] **Step 9: Make release script executable**

Run:

```bash
chmod +x scripts/build-release.sh
```

- [ ] **Step 10: Resolve dependencies**

Run:

```bash
go mod tidy
```

Expected: no errors are printed. `go.sum` may not exist yet because external dependencies are added in later tasks when their imports first appear.

- [ ] **Step 11: Run tests, builds, and offline daemon artifact build**

Run:

```bash
go test ./...
go build ./cmd/remork ./cmd/remorkd
scripts/build-release.sh dev
dist/remorkd-$(go env GOOS)-$(go env GOARCH) --version
```

Expected: all commands pass. `dist/checksums.txt` exists, and the local-platform `remorkd` binary prints `remorkd dev`.

- [ ] **Step 12: Commit**

```bash
git add go.mod .gitignore scripts deploy cmd internal
git commit -m "chore: bootstrap remork go module"
```

## Task 1: Path Safety And Workspace Resolution

**Files:**
- Create: `internal/paths/paths.go`
- Create: `internal/paths/paths_test.go`

- [ ] **Step 1: Write failing path safety tests**

Create `internal/paths/paths_test.go`:

```go
package paths

import "testing"

func TestResolveInsideWorkspaceAcceptsCleanRelativePath(t *testing.T) {
	got, err := ResolveInsideWorkspace("/srv/project", "src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/srv/project/src/main.go"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveInsideWorkspaceRejectsParentTraversal(t *testing.T) {
	_, err := ResolveInsideWorkspace("/srv/project", "../secret")
	if err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestResolveInsideWorkspaceRejectsAbsoluteEscape(t *testing.T) {
	_, err := ResolveInsideWorkspace("/srv/project", "/etc/passwd")
	if err == nil {
		t.Fatal("expected absolute escape error")
	}
}

func TestNormalizeRemotePathUsesSlashAndNoLeadingSlash(t *testing.T) {
	got, err := NormalizeRemotePath("./src//main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "src/main.go" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeRemotePathRejectsEmptyRootEscape(t *testing.T) {
	for _, input := range []string{"", ".", "..", "../x", "/x"} {
		if _, err := NormalizeRemotePath(input); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/paths -run Test -v
```

Expected: FAIL with undefined `ResolveInsideWorkspace`.

- [ ] **Step 3: Implement path safety**

Create `internal/paths/paths.go`:

```go
package paths

import (
	"errors"
	"path/filepath"
	"strings"
)

var ErrPathEscape = errors.New("path escapes workspace")

func NormalizeRemotePath(p string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(p))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", ErrPathEscape
	}
	return clean, nil
}

func ResolveInsideWorkspace(root string, remotePath string) (string, error) {
	if filepath.IsAbs(remotePath) {
		return "", ErrPathEscape
	}
	norm, err := NormalizeRemotePath(remotePath)
	if err != nil {
		return "", err
	}
	rootClean, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(rootClean, filepath.FromSlash(norm))
	rel, err := filepath.Rel(rootClean, joined)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathEscape
	}
	return joined, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/paths -run Test -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/paths
git commit -m "feat: add workspace path safety"
```

## Task 2: Manifest Scanner And Large-File Metadata

**Files:**
- Create: `internal/manifest/manifest.go`
- Create: `internal/manifest/manifest_test.go`
- Modify: `internal/api/types.go`

- [ ] **Step 1: Write failing manifest tests**

Create `internal/manifest/manifest_test.go`:

```go
package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScanClassifiesSmallAndLargeFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "small.txt"), []byte("hello"))
	mustWrite(t, filepath.Join(root, "large.bin"), []byte("1234567890"))

	got, err := Scan(root, ".", Options{LargeThreshold: 5})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	small := findEntry(t, got.Entries, "small.txt")
	if small.Large {
		t.Fatal("small.txt should not be large")
	}
	if small.Hash == "" {
		t.Fatal("small.txt should have hash")
	}

	large := findEntry(t, got.Entries, "large.bin")
	if !large.Large {
		t.Fatal("large.bin should be large")
	}
}

func TestScanSkipsDotGitDirectory(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".git", "config"), []byte("private"))
	mustWrite(t, filepath.Join(root, "src", "main.go"), []byte("package main"))

	got, err := Scan(root, ".", Options{LargeThreshold: 128 << 20})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, e := range got.Entries {
		if e.Path == ".git/config" {
			t.Fatal("manifest must not include project .git internals")
		}
	}
}

func TestLargeMetaJSONIsStableAndReadable(t *testing.T) {
	entry := EntryForTest("checkpoints/model.tar.gz", 200, true)
	meta := BuildLargeMeta("lab:/workspace", entry)
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) == "" {
		t.Fatal("empty json")
	}
	if meta.PullCommand != "remork pull lab:/workspace/checkpoints/model.tar.gz" {
		t.Fatalf("pull command %q", meta.PullCommand)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/manifest -run Test -v
```

Expected: FAIL with undefined `Scan`.

- [ ] **Step 3: Implement scanner**

Create `internal/manifest/manifest.go`:

```go
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"remork/internal/api"
)

type Options struct {
	LargeThreshold int64
}

func Scan(root string, path string, opts Options) (api.ManifestResponse, error) {
	if opts.LargeThreshold <= 0 {
		opts.LargeThreshold = 128 << 20
	}
	start := filepath.Join(root, path)
	var entries []api.FileEntry
	err := filepath.WalkDir(start, func(p string, d os.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == ".remork") {
			return filepath.SkipDir
		}
		info, infoErr := d.Info()
		if walkErr != nil || infoErr != nil {
			entries = append(entries, api.FileEntry{Path: rel, Error: firstErrString(walkErr, infoErr)})
			return nil
		}
		entry := api.FileEntry{
			Path:        rel,
			Size:        info.Size(),
			ModTimeUnix: info.ModTime().Unix(),
			Revision:    revisionFor(info),
		}
		switch {
		case d.Type().IsRegular():
			entry.Type = api.FileTypeFile
			entry.Large = info.Size() > opts.LargeThreshold
			if !entry.Large {
				hash, err := hashFile(p)
				if err != nil {
					entry.Error = err.Error()
				} else {
					entry.Hash = hash
				}
			}
		case d.IsDir():
			entry.Type = api.FileTypeDir
		case d.Type()&os.ModeSymlink != 0:
			entry.Type = api.FileTypeSymlink
		default:
			entry.Type = api.FileTypeSpecial
		}
		entries = append(entries, entry)
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return api.ManifestResponse{
		Root:      root,
		Path:      path,
		Revision:  manifestRevision(entries),
		Entries:   entries,
		Threshold: opts.LargeThreshold,
	}, err
}

func BuildLargeMeta(workspaceRef string, entry api.FileEntry) api.LargeFileMeta {
	return api.LargeFileMeta{
		Kind:        "remote-large-file",
		RemotePath:  "/" + strings.TrimPrefix(entry.Path, "/"),
		Size:        entry.Size,
		ModTimeUnix: entry.ModTimeUnix,
		Hash:        entry.Hash,
		Revision:    entry.Revision,
		Pulled:      false,
		PullCommand: "remork pull " + strings.TrimRight(workspaceRef, "/") + "/" + entry.Path,
	}
}

func EntryForTest(path string, size int64, large bool) api.FileEntry {
	return api.FileEntry{Path: path, Size: size, Large: large, Revision: "rev-test"}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func revisionFor(info os.FileInfo) string {
	return strings.Join([]string{info.ModTime().UTC().Format("20060102150405"), int64String(info.Size())}, "-")
}

func manifestRevision(entries []api.FileEntry) string {
	h := sha256.New()
	for _, e := range entries {
		io.WriteString(h, e.Path)
		io.WriteString(h, e.Revision)
		io.WriteString(h, e.Hash)
	}
	return "rev-" + hex.EncodeToString(h.Sum(nil))[:16]
}

func firstErrString(a, b error) string {
	if a != nil {
		return a.Error()
	}
	if b != nil {
		return b.Error()
	}
	return ""
}

func int64String(v int64) string {
	return strconvFormatInt(v)
}
```

Add the missing import helper at the bottom of the same file:

```go
func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
```

Add `strconv` to the import list.

- [ ] **Step 4: Add test helper for lookup**

Append to `internal/manifest/manifest_test.go`:

```go
func findEntry(t *testing.T, entries []api.FileEntry, path string) api.FileEntry {
	t.Helper()
	for _, e := range entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("entry %q not found in %#v", path, entries)
	return api.FileEntry{}
}
```

Add this import to `internal/manifest/manifest_test.go`:

```go
"remork/internal/api"
```

- [ ] **Step 5: Run test and fix compile errors**

Run:

```bash
gofmt -w internal/manifest internal/api
go test ./internal/manifest -run Test -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/manifest internal/api
git commit -m "feat: add manifest scanning and large file metadata"
```

## Task 3: Local State Store And Dirty Detection

**Files:**
- Create: `internal/state/state.go`
- Create: `internal/state/state_test.go`

- [ ] **Step 1: Write failing state tests**

Create `internal/state/state_test.go`:

```go
package state

import (
	"os"
	"path/filepath"
	"testing"

	"remork/internal/api"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"src/main.go": {Path: "src/main.go", BaseHash: "sha256:a", Revision: "rev-a", Large: false},
	}}
	if err := store.Save(snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Load("lab:/workspace")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Entries["src/main.go"].BaseHash != "sha256:a" {
		t.Fatalf("bad hash: %#v", got)
	}
}

func TestDirtyDetectionFindsModifyCreateDelete(t *testing.T) {
	local := t.TempDir()
	mustWrite(t, filepath.Join(local, "changed.txt"), []byte("after"))
	mustWrite(t, filepath.Join(local, "new.txt"), []byte("new"))

	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"changed.txt": {Path: "changed.txt", BaseHash: HashBytes([]byte("before")), Type: api.FileTypeFile},
		"deleted.txt": {Path: "deleted.txt", BaseHash: HashBytes([]byte("gone")), Type: api.FileTypeFile},
	}}
	dirty, err := DetectDirty(local, snap)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	assertChange(t, dirty, "changed.txt", ChangeModify)
	assertChange(t, dirty, "new.txt", ChangeCreate)
	assertChange(t, dirty, "deleted.txt", ChangeDelete)
}

func TestDetectDirtyIgnoresMetaPlaceholderEdits(t *testing.T) {
	local := t.TempDir()
	mustWrite(t, filepath.Join(local, "large.bin.meta"), []byte("edited"))
	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"large.bin": {Path: "large.bin", Large: true, MetaPath: "large.bin.meta", Type: api.FileTypeFile},
	}}
	dirty, err := DetectDirty(local, snap)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	if len(dirty) != 0 {
		t.Fatalf("meta placeholder edits must not apply: %#v", dirty)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/state -run Test -v
```

Expected: FAIL with undefined `NewStore`.

- [ ] **Step 3: Implement state store**

Create `internal/state/state.go`:

```go
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"remork/internal/api"
)

type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeModify ChangeKind = "modify"
	ChangeDelete ChangeKind = "delete"
)

type TrackedFile struct {
	Path     string       `json:"path"`
	MetaPath string      `json:"meta_path,omitempty"`
	Type     api.FileType `json:"type"`
	BaseHash string       `json:"base_hash,omitempty"`
	Revision string      `json:"revision"`
	Large    bool         `json:"large"`
}

type Snapshot struct {
	WorkspaceRef string                 `json:"workspace_ref"`
	Entries      map[string]TrackedFile `json:"entries"`
}

type DirtyChange struct {
	Path string
	Kind ChangeKind
}

type Store struct {
	dir string
}

func NewStore(dir string) Store {
	return Store{dir: dir}
}

func (s Store) Save(snapshot Snapshot) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(snapshot.WorkspaceRef)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s Store) Load(workspaceRef string) (Snapshot, error) {
	data, err := os.ReadFile(s.path(workspaceRef))
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	err = json.Unmarshal(data, &snap)
	return snap, err
}

func (s Store) path(workspaceRef string) string {
	name := strings.NewReplacer("/", "_", ":", "_").Replace(workspaceRef)
	return filepath.Join(s.dir, name+".json")
}

func DetectDirty(localRoot string, snap Snapshot) ([]DirtyChange, error) {
	var changes []DirtyChange
	seen := map[string]bool{}
	for path, tracked := range snap.Entries {
		if tracked.Large && tracked.MetaPath != "" {
			continue
		}
		seen[path] = true
		full := filepath.Join(localRoot, filepath.FromSlash(path))
		_, err := os.Stat(full)
		if os.IsNotExist(err) {
			changes = append(changes, DirtyChange{Path: path, Kind: ChangeDelete})
			continue
		}
		if err != nil {
			return nil, err
		}
		hash, err := HashFile(full)
		if err != nil {
			return nil, err
		}
		if tracked.BaseHash != "" && hash != tracked.BaseHash {
			changes = append(changes, DirtyChange{Path: path, Kind: ChangeModify})
		}
	}
	err := filepath.WalkDir(localRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".remork" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(localRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".meta") {
			return nil
		}
		if !seen[rel] {
			changes = append(changes, DirtyChange{Path: rel, Kind: ChangeCreate})
		}
		return nil
	})
	return changes, err
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
```

- [ ] **Step 4: Add test helper**

Append to `internal/state/state_test.go`:

```go
func assertChange(t *testing.T, changes []DirtyChange, path string, kind ChangeKind) {
	t.Helper()
	for _, c := range changes {
		if c.Path == path && c.Kind == kind {
			return
		}
	}
	t.Fatalf("missing change %s %s in %#v", kind, path, changes)
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w internal/state
go test ./internal/state -run Test -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/state
git commit -m "feat: add local state and dirty detection"
```

## Task 4: Sync/Pull Planner

**Files:**
- Create: `internal/planner/planner.go`
- Create: `internal/planner/planner_test.go`

- [ ] **Step 1: Write failing planner tests**

Create `internal/planner/planner_test.go`:

```go
package planner

import (
	"testing"

	"remork/internal/api"
	"remork/internal/state"
)

func TestSyncPlansSmallDownloadAndLargeMeta(t *testing.T) {
	manifest := api.ManifestResponse{Threshold: 128, Entries: []api.FileEntry{
		{Path: "a.txt", Type: api.FileTypeFile, Size: 5, Hash: "sha256:a", Revision: "rev-a"},
		{Path: "big.tar.gz", Type: api.FileTypeFile, Size: 200, Large: true, Revision: "rev-big"},
	}}
	plan := PlanSync(manifest, state.Snapshot{}, Options{WorkspaceRef: "lab:/workspace"})
	assertOp(t, plan, "a.txt", OpDownload)
	assertOp(t, plan, "big.tar.gz", OpWriteMeta)
}

func TestSyncDoesNotOverwriteDirtyFile(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "a.txt", Type: api.FileTypeFile, Size: 6, Hash: "sha256:remote", Revision: "rev-remote"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"a.txt": {Path: "a.txt", BaseHash: "sha256:base", Revision: "rev-base"},
	}}
	plan := PlanSync(manifest, snap, Options{Dirty: []state.DirtyChange{{Path: "a.txt", Kind: state.ChangeModify}}})
	assertOp(t, plan, "a.txt", OpConflict)
}

func TestPullIncludeLargeDownloadsLargeFile(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "big.tar.gz", Type: api.FileTypeFile, Size: 200, Large: true, Revision: "rev-big"},
	}}
	plan := PlanPull(manifest, state.Snapshot{}, Options{IncludeLarge: true, TargetPath: "big.tar.gz"})
	assertOp(t, plan, "big.tar.gz", OpDownload)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/planner -run Test -v
```

Expected: FAIL with undefined `PlanSync`.

- [ ] **Step 3: Implement planner**

Create `internal/planner/planner.go`:

```go
package planner

import (
	"remork/internal/api"
	"remork/internal/state"
)

type OperationKind string

const (
	OpDownload  OperationKind = "download"
	OpWriteMeta OperationKind = "write_meta"
	OpDelete    OperationKind = "delete"
	OpConflict  OperationKind = "conflict"
	OpSkip      OperationKind = "skip"
)

type Operation struct {
	Path string
	Kind OperationKind
	Reason string
	Entry api.FileEntry
}

type Plan struct {
	Operations []Operation
}

type Options struct {
	WorkspaceRef string
	TargetPath string
	IncludeLarge bool
	Force bool
	Dirty []state.DirtyChange
}

func PlanSync(manifest api.ManifestResponse, snap state.Snapshot, opts Options) Plan {
	dirty := dirtySet(opts.Dirty)
	var ops []Operation
	for _, entry := range manifest.Entries {
		if entry.Type != api.FileTypeFile {
			continue
		}
		if dirty[entry.Path] && !opts.Force {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpConflict, Reason: "local dirty", Entry: entry})
			continue
		}
		tracked, exists := snap.Entries[entry.Path]
		if exists && tracked.Revision == entry.Revision && !opts.Force {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpSkip, Reason: "current", Entry: entry})
			continue
		}
		if entry.Large {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpWriteMeta, Entry: entry})
		} else {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpDownload, Entry: entry})
		}
	}
	return Plan{Operations: ops}
}

func PlanPull(manifest api.ManifestResponse, snap state.Snapshot, opts Options) Plan {
	opts.Force = opts.Force
	var ops []Operation
	for _, entry := range manifest.Entries {
		if entry.Type != api.FileTypeFile {
			continue
		}
		if opts.TargetPath != "" && entry.Path != opts.TargetPath {
			continue
		}
		if entry.Large && !opts.IncludeLarge {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpWriteMeta, Entry: entry})
		} else {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpDownload, Entry: entry})
		}
	}
	return Plan{Operations: ops}
}

func dirtySet(changes []state.DirtyChange) map[string]bool {
	out := map[string]bool{}
	for _, c := range changes {
		out[c.Path] = true
	}
	return out
}
```

- [ ] **Step 4: Add test helper**

Append to `internal/planner/planner_test.go`:

```go
func assertOp(t *testing.T, plan Plan, path string, kind OperationKind) {
	t.Helper()
	for _, op := range plan.Operations {
		if op.Path == path && op.Kind == kind {
			return
		}
	}
	t.Fatalf("missing op %s %s in %#v", kind, path, plan.Operations)
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w internal/planner
go test ./internal/planner -run Test -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/planner
git commit -m "feat: add sync and pull planner"
```

## Task 5: Daemon Manifest And Download API

**Files:**
- Create: `internal/daemon/server.go`
- Create: `internal/daemon/server_test.go`
- Create: `internal/client/client.go`
- Create: `internal/client/client_test.go`

- [ ] **Step 1: Write failing daemon API tests**

Create `internal/daemon/server_test.go`:

```go
package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestEndpointReturnsEntries(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("hello"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}, LargeThreshold: 128 << 20}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest?root=" + root + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "a.txt") {
		t.Fatalf("body missing a.txt: %s", body)
	}
}

func TestDownloadRejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/download?root=" + root + "&path=../secret")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestDownloadSupportsRange(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("abcdef"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/download?root="+root+"&path=a.txt", nil)
	req.Header.Set("Range", "bytes=1-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "bcd" {
		t.Fatalf("range body %q", body)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/daemon -run Test -v
```

Expected: FAIL with undefined `NewServer`.

- [ ] **Step 3: Implement daemon server**

Create `internal/daemon/server.go`:

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"os"

	"remork/internal/manifest"
	"remork/internal/paths"
)

type Config struct {
	Roots []string
	LargeThreshold int64
}

type Server struct {
	cfg Config
	mux *http.ServeMux
}

func NewServer(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("/manifest", s.handleManifest)
	s.mux.HandleFunc("/download", s.handleDownload)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}
	resp, err := manifest.Scan(root, path, manifest.Options{LargeThreshold: s.cfg.LargeThreshold})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	path := r.URL.Query().Get("path")
	full, err := paths.ResolveInsideWorkspace(root, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (s *Server) allowedRoot(root string) bool {
	for _, r := range s.cfg.Roots {
		if r == root {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run daemon tests**

Run:

```bash
gofmt -w internal/daemon
go test ./internal/daemon -run Test -v
```

Expected: PASS.

- [ ] **Step 5: Write failing client tests**

Create `internal/client/client_test.go`:

```go
package client

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/daemon"
)

func TestClientManifestAndDownload(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := New(srv.URL)
	manifest, err := c.Manifest(root, ".")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("empty manifest")
	}
	data, err := c.Download(root, "a.txt")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("data %q", data)
	}
}
```

- [ ] **Step 6: Implement client**

Create `internal/client/client.go`:

```go
package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"remork/internal/api"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) Client {
	return Client{base: base, http: http.DefaultClient}
}

func (c Client) Manifest(root, path string) (api.ManifestResponse, error) {
	u, _ := url.Parse(c.base + "/manifest")
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	q.Set("recursive", "true")
	u.RawQuery = q.Encode()
	resp, err := c.http.Get(u.String())
	if err != nil {
		return api.ManifestResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return api.ManifestResponse{}, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var out api.ManifestResponse
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

func (c Client) Download(root, path string) ([]byte, error) {
	u, _ := url.Parse(c.base + "/download")
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	u.RawQuery = q.Encode()
	resp, err := c.http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return io.ReadAll(resp.Body)
}

type HTTPError struct {
	StatusCode int
	Body string
}

func (e *HTTPError) Error() string {
	return e.Body
}
```

- [ ] **Step 7: Run client and daemon tests**

Run:

```bash
gofmt -w internal/client internal/daemon
go test ./internal/client ./internal/daemon -run Test -v
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/client internal/daemon
git commit -m "feat: add daemon manifest and download APIs"
```

## Task 6: Sync And Pull File Materialization

**Files:**
- Create: `internal/transfer/transfer.go`
- Create: `internal/transfer/transfer_test.go`
- Create: `internal/progress/progress.go`
- Create: `internal/progress/progress_test.go`
- Modify: `internal/client/client.go`

- [ ] **Step 1: Write failing transfer tests**

Create `internal/transfer/transfer_test.go`:

```go
package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/api"
)

func TestWriteDownloadedFileCreatesParentsAndContent(t *testing.T) {
	root := t.TempDir()
	err := WriteFile(root, "src/main.go", []byte("package main"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "src", "main.go"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "package main" {
		t.Fatalf("data %q", data)
	}
}

func TestWriteLargeMetaUsesOriginalNamePlusMeta(t *testing.T) {
	root := t.TempDir()
	meta := api.LargeFileMeta{Kind: "remote-large-file", RemotePath: "/big.tar.gz", Size: 200, PullCommand: "remork pull lab:/w/big.tar.gz"}
	if err := WriteLargeMeta(root, "big.tar.gz", meta); err != nil {
		t.Fatalf("meta: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "big.tar.gz.meta"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded api.LargeFileMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json: %v", err)
	}
	if decoded.PullCommand == "" {
		t.Fatal("missing pull command")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/transfer -run Test -v
```

Expected: FAIL with undefined `WriteFile`.

- [ ] **Step 3: Implement transfer writes**

Create `internal/transfer/transfer.go`:

```go
package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteFile(localRoot, remotePath string, data []byte) error {
	full := filepath.Join(localRoot, filepath.FromSlash(remotePath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp := full + ".remork-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

func WriteLargeMeta(localRoot, remotePath string, meta any) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(localRoot, remotePath+".meta", data)
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/transfer
go test ./internal/transfer -run Test -v
```

Expected: PASS.

- [ ] **Step 5: Add range download test**

Append to `internal/client/client_test.go`:

```go
func TestClientDownloadRange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := New(srv.URL)
	data, err := c.DownloadRange(root, "a.txt", 2, 4)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if string(data) != "cde" {
		t.Fatalf("data %q", data)
	}
}
```

- [ ] **Step 6: Implement range client**

Append to `internal/client/client.go`:

```go
func (c Client) DownloadRange(root, path string, start, end int64) ([]byte, error) {
	u, _ := url.Parse(c.base + "/download")
	q := u.Query()
	q.Set("root", root)
	q.Set("path", path)
	u.RawQuery = q.Encode()
	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return io.ReadAll(resp.Body)
}
```

Add `fmt` to `internal/client/client.go` imports.

- [ ] **Step 7: Add progress reporter tests**

Create `internal/progress/progress_test.go`:

```go
package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestTextReporterShowsProgressWhenInteractive(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, Options{Quiet: false})
	r.Start("download", 100)
	r.Advance(40)
	r.Advance(60)
	r.Done()
	out := buf.String()
	if !strings.Contains(out, "download") || !strings.Contains(out, "100/100") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestTextReporterQuietSuppressesOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, Options{Quiet: true})
	r.Start("download", 100)
	r.Advance(100)
	r.Done()
	if buf.String() != "" {
		t.Fatalf("quiet output: %q", buf.String())
	}
}
```

- [ ] **Step 8: Implement progress reporter**

Create `internal/progress/progress.go`:

```go
package progress

import (
	"fmt"
	"io"
)

type Options struct {
	Quiet bool
}

type TextReporter struct {
	w io.Writer
	quiet bool
	label string
	total int64
	current int64
}

func NewTextReporter(w io.Writer, opts Options) *TextReporter {
	return &TextReporter{w: w, quiet: opts.Quiet}
}

func (r *TextReporter) Start(label string, total int64) {
	r.label = label
	r.total = total
	r.current = 0
	r.print()
}

func (r *TextReporter) Advance(delta int64) {
	r.current += delta
	if r.current > r.total {
		r.current = r.total
	}
	r.print()
}

func (r *TextReporter) Done() {
	r.current = r.total
	r.print()
}

func (r *TextReporter) print() {
	if r.quiet || r.w == nil {
		return
	}
	fmt.Fprintf(r.w, "%s %d/%d\n", r.label, r.current, r.total)
}
```

- [ ] **Step 9: Run tests**

Run:

```bash
gofmt -w internal/client internal/transfer internal/progress
go test ./internal/client ./internal/transfer ./internal/progress -run Test -v
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/client internal/transfer internal/progress
git commit -m "feat: add transfer materialization"
```

## Task 7: Apply Changesets With Base Verification

**Files:**
- Create: `internal/apply/apply.go`
- Create: `internal/apply/apply_test.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/client/client.go`
- Modify: `internal/api/types.go`

- [ ] **Step 1: Write failing apply tests**

Create `internal/apply/apply_test.go`:

```go
package apply

import (
	"os"
	"path/filepath"
	"testing"

	"remork/internal/state"
)

func TestApplyUpdateSucceedsWhenBaseMatches(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before"))
	change := Change{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("before")), Content: []byte("after")}
	result, err := Apply(root, Changeset{Changes: []Change{change}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !result.Applied {
		t.Fatalf("not applied: %#v", result)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "after" {
		t.Fatalf("data %q", data)
	}
}

func TestApplyRejectsWhenRemoteChanged(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("remote"))
	change := Change{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("base")), Content: []byte("after")}
	result, err := Apply(root, Changeset{Changes: []Change{change}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Applied {
		t.Fatal("must not apply conflict")
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "remote" {
		t.Fatalf("remote overwritten: %q", data)
	}
}

func TestApplyCreateAndDelete(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "delete.txt"), []byte("gone"))
	cs := Changeset{Changes: []Change{
		{Path: "new.txt", Kind: ChangeCreate, Content: []byte("new")},
		{Path: "delete.txt", Kind: ChangeDelete, BaseHash: state.HashBytes([]byte("gone"))},
	}}
	if _, err := Apply(root, cs); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete still exists: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/apply -run Test -v
```

Expected: FAIL with undefined `Apply`.

- [ ] **Step 3: Implement apply package**

Create `internal/apply/apply.go`:

```go
package apply

import (
	"errors"
	"os"
	"path/filepath"

	"remork/internal/paths"
	"remork/internal/state"
)

type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeDelete ChangeKind = "delete"
)

type Change struct {
	Path string `json:"path"`
	Kind ChangeKind `json:"kind"`
	BaseHash string `json:"base_hash,omitempty"`
	Content []byte `json:"content,omitempty"`
}

type Changeset struct {
	ID string `json:"id,omitempty"`
	Changes []Change `json:"changes"`
}

type Result struct {
	Applied bool `json:"applied"`
	Conflicts []string `json:"conflicts,omitempty"`
}

var ErrConflict = errors.New("apply conflict")

func Apply(root string, cs Changeset) (Result, error) {
	var conflicts []string
	for _, ch := range cs.Changes {
		full, err := paths.ResolveInsideWorkspace(root, ch.Path)
		if err != nil {
			return Result{Applied: false}, err
		}
		switch ch.Kind {
		case ChangeCreate:
			if _, err := os.Stat(full); err == nil {
				conflicts = append(conflicts, ch.Path)
			}
		case ChangeUpdate, ChangeDelete:
			hash, err := state.HashFile(full)
			if err != nil || hash != ch.BaseHash {
				conflicts = append(conflicts, ch.Path)
			}
		}
	}
	if len(conflicts) > 0 {
		return Result{Applied: false, Conflicts: conflicts}, ErrConflict
	}
	for _, ch := range cs.Changes {
		full, err := paths.ResolveInsideWorkspace(root, ch.Path)
		if err != nil {
			return Result{Applied: false}, err
		}
		switch ch.Kind {
		case ChangeCreate, ChangeUpdate:
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return Result{Applied: false}, err
			}
			tmp := full + ".remork-apply"
			if err := os.WriteFile(tmp, ch.Content, 0o644); err != nil {
				return Result{Applied: false}, err
			}
			if err := os.Rename(tmp, full); err != nil {
				return Result{Applied: false}, err
			}
		case ChangeDelete:
			if err := os.Remove(full); err != nil {
				return Result{Applied: false}, err
			}
		}
	}
	return Result{Applied: true}, nil
}
```

- [ ] **Step 4: Add test helper**

Append to `internal/apply/apply_test.go`:

```go
func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 5: Run apply tests**

Run:

```bash
gofmt -w internal/apply
go test ./internal/apply -run Test -v
```

Expected: PASS.

- [ ] **Step 6: Wire daemon `/apply` endpoint and client test**

Add a daemon/client integration test in `internal/client/client_test.go`:

```go
func TestClientApplyUpdate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := New(srv.URL)
	res, err := c.Apply(root, apply.Changeset{Changes: []apply.Change{
		{Path: "a.txt", Kind: apply.ChangeUpdate, BaseHash: state.HashBytes([]byte("before")), Content: []byte("after")},
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied {
		t.Fatalf("not applied: %#v", res)
	}
}
```

Add imports:

```go
"remork/internal/apply"
"remork/internal/state"
```

- [ ] **Step 7: Implement endpoint and client**

Add to `internal/daemon/server.go` in `NewServer`:

```go
s.mux.HandleFunc("/apply", s.handleApply)
```

Append:

```go
func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	var cs apply.Changeset
	if err := json.NewDecoder(r.Body).Decode(&cs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := apply.Apply(root, cs)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(result)
		return
	}
	json.NewEncoder(w).Encode(result)
}
```

Add `remork/internal/apply` to imports.

Append to `internal/client/client.go`:

```go
func (c Client) Apply(root string, cs apply.Changeset) (apply.Result, error) {
	u, _ := url.Parse(c.base + "/apply")
	q := u.Query()
	q.Set("root", root)
	u.RawQuery = q.Encode()
	data, err := json.Marshal(cs)
	if err != nil {
		return apply.Result{}, err
	}
	resp, err := c.http.Post(u.String(), "application/json", bytes.NewReader(data))
	if err != nil {
		return apply.Result{}, err
	}
	defer resp.Body.Close()
	var result apply.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return apply.Result{}, err
	}
	if resp.StatusCode >= 300 {
		return result, &HTTPError{StatusCode: resp.StatusCode, Body: "apply failed"}
	}
	return result, nil
}
```

Add imports to `internal/client/client.go`:

```go
"bytes"
"remork/internal/apply"
```

- [ ] **Step 8: Run tests**

Run:

```bash
gofmt -w internal/apply internal/client internal/daemon
go test ./internal/apply ./internal/client ./internal/daemon -run Test -v
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/apply internal/client internal/daemon
git commit -m "feat: add explicit apply with base verification"
```

## Task 8: Safe Exec

**Files:**
- Create: `internal/exec/exec.go`
- Create: `internal/exec/exec_test.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/client/client.go`
- Modify: `internal/api/types.go`

- [ ] **Step 1: Write failing exec tests**

Create `internal/exec/exec_test.go`:

```go
package execx

import (
	"testing"
	"time"
)

func TestRunCapturesStdoutAndExitCode(t *testing.T) {
	res, err := Run(Options{Command: []string{"sh", "-c", "echo hello"}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 || res.Stdout != "hello\n" {
		t.Fatalf("bad result: %#v", res)
	}
}

func TestRunTimeoutKillsCommand(t *testing.T) {
	res, err := Run(Options{Command: []string{"sh", "-c", "sleep 2"}, Timeout: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !res.TimedOut {
		t.Fatalf("expected timed out result: %#v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/exec -run Test -v
```

Expected: FAIL with undefined `Run`.

- [ ] **Step 3: Implement exec runner**

Create `internal/exec/exec.go`:

```go
package execx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

type Options struct {
	Cwd string
	Command []string
	Env []string
	Timeout time.Duration
}

type Result struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	ExitCode int `json:"exit_code"`
	TimedOut bool `json:"timed_out"`
}

func Run(opts Options) (Result, error) {
	if len(opts.Command) == 0 {
		return Result{ExitCode: -1}, errors.New("empty command")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = append(cmd.Env, opts.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		res.TimedOut = true
		return res, ctx.Err()
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	return res, err
}
```

- [ ] **Step 4: Run exec tests**

Run:

```bash
gofmt -w internal/exec
go test ./internal/exec -run Test -v
```

Expected: PASS.

- [ ] **Step 5: Wire `/exec` endpoint**

Add API structs to `internal/api/types.go`:

```go
type ExecRequest struct {
	Root string `json:"root"`
	Cwd string `json:"cwd"`
	Command []string `json:"command"`
	Env []string `json:"env,omitempty"`
	TimeoutMillis int64 `json:"timeout_millis,omitempty"`
}
```

Add daemon endpoint test to `internal/daemon/server_test.go`:

```go
func TestExecEndpointRunsCommand(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	body := strings.NewReader(`{"root":"` + root + `","cwd":"` + root + `","command":["sh","-c","pwd"]}`)
	resp, err := http.Post(srv.URL+"/exec", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(data), root) {
		t.Fatalf("body: %s", data)
	}
}
```

- [ ] **Step 6: Implement endpoint**

Add in `NewServer`:

```go
s.mux.HandleFunc("/exec", s.handleExec)
```

Append to `internal/daemon/server.go`:

```go
func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req api.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.allowedRoot(req.Root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	cwd, err := paths.ResolveInsideWorkspace(req.Root, strings.TrimPrefix(req.Cwd, req.Root+"/"))
	if err != nil && req.Cwd != req.Root {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Cwd == req.Root {
		cwd = req.Root
	}
	timeout := time.Duration(req.TimeoutMillis) * time.Millisecond
	result, runErr := execx.Run(execx.Options{Cwd: cwd, Command: req.Command, Env: req.Env, Timeout: timeout})
	if runErr != nil && !result.TimedOut && result.ExitCode == 0 {
		result.ExitCode = 1
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
```

Add imports:

```go
"strings"
"time"
"remork/internal/api"
"remork/internal/exec"
```

Alias the exec import:

```go
execx "remork/internal/exec"
```

- [ ] **Step 7: Run tests**

Run:

```bash
gofmt -w internal/api internal/daemon internal/exec
go test ./internal/exec ./internal/daemon -run Test -v
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/api internal/daemon internal/exec
git commit -m "feat: add remote exec endpoint"
```

## Task 9: PTY Shell Sessions

**Files:**
- Create: `internal/pty/session.go`
- Create: `internal/pty/session_test.go`
- Modify: `internal/daemon/server.go`

- [ ] **Step 1: Write failing PTY manager tests**

Create `internal/pty/session_test.go`:

```go
package ptysession

import (
	"testing"
	"time"
)

func TestManagerStartsListsAndClosesSession(t *testing.T) {
	m := NewManager(100 * time.Millisecond)
	s, err := m.Start(StartOptions{Command: []string{"sh"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(m.List()) != 1 {
		t.Fatal("missing session")
	}
	if err := m.Close(s.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("session not removed")
	}
}

func TestManagerReapsIdleSession(t *testing.T) {
	m := NewManager(10 * time.Millisecond)
	_, err := m.Start(StartOptions{Command: []string{"sh"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	m.ReapIdle()
	if len(m.List()) != 0 {
		t.Fatal("idle session not reaped")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/pty -run Test -v
```

Expected: FAIL with undefined `NewManager`.

- [ ] **Step 3: Implement PTY manager**

Add the PTY dependency:

```bash
go get github.com/creack/pty@v1.1.24
```

Create `internal/pty/session.go`:

```go
package ptysession

import (
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

type StartOptions struct {
	Command []string
	Cwd string
	Env []string
	Rows uint16
	Cols uint16
}

type Session struct {
	ID string
	Command []string
	LastActive time.Time
	cmd *exec.Cmd
	file *os.File
}

type Manager struct {
	mu sync.Mutex
	retention time.Duration
	sessions map[string]*Session
}

func NewManager(retention time.Duration) *Manager {
	return &Manager{retention: retention, sessions: map[string]*Session{}}
}

func (m *Manager) Start(opts StartOptions) (*Session, error) {
	if len(opts.Command) == 0 {
		opts.Command = []string{"sh"}
	}
	cmd := exec.Command(opts.Command[0], opts.Command[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = append(cmd.Env, opts.Env...)
	rows, cols := opts.Rows, opts.Cols
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	s := &Session{ID: randomID(), Command: opts.Command, LastActive: time.Now(), cmd: cmd, file: f}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, Session{ID: s.ID, Command: s.Command, LastActive: s.LastActive})
	}
	return out
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	_ = s.file.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

func (m *Manager) ReapIdle() {
	for _, s := range m.List() {
		if time.Since(s.LastActive) > m.retention {
			_ = m.Close(s.ID)
		}
	}
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
```

Add missing import:

```go
"os"
```

- [ ] **Step 4: Run PTY tests**

Run:

```bash
gofmt -w internal/pty
go test ./internal/pty -run Test -v
```

Expected: PASS on macOS/Linux. If CI lacks a PTY, skip PTY tests only when `os.Getenv("REMORK_SKIP_PTY_TESTS") == "1"` is set.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/pty
git commit -m "feat: add pty session manager"
```

## Task 10: Watch Events And Reconciliation Signals

**Files:**
- Create: `internal/watch/watch.go`
- Create: `internal/watch/watch_test.go`
- Modify: `internal/daemon/server.go`

- [ ] **Step 1: Write failing watch tests**

Create `internal/watch/watch_test.go`:

```go
package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherEmitsCreateUpdateDelete(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatalf("watcher: %v", err)
	}
	defer w.Close()
	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, w.Events(), "a.txt")
	if ev.Path != "a.txt" {
		t.Fatalf("event %#v", ev)
	}
}

func TestWatcherOverflowEventCanBeInjectedForReconcile(t *testing.T) {
	ev := Overflow()
	if ev.Kind != EventOverflow || !ev.ResyncRequired {
		t.Fatalf("bad overflow event: %#v", ev)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/watch -run Test -v
```

Expected: FAIL with undefined `New`.

- [ ] **Step 3: Implement watcher**

Add the watcher dependency:

```bash
go get github.com/fsnotify/fsnotify@v1.9.0
```

Create `internal/watch/watch.go`:

```go
package watch

import (
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type EventKind string

const (
	EventCreate EventKind = "create"
	EventUpdate EventKind = "update"
	EventDelete EventKind = "delete"
	EventRename EventKind = "rename"
	EventOverflow EventKind = "overflow"
)

type Event struct {
	Kind EventKind `json:"kind"`
	Path string `json:"path,omitempty"`
	Revision string `json:"revision"`
	ResyncRequired bool `json:"resync_required,omitempty"`
}

type Watcher struct {
	root string
	fs *fsnotify.Watcher
	events chan Event
	done chan struct{}
}

func New(root string) (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{root: root, fs: fs, events: make(chan Event, 32), done: make(chan struct{})}, nil
}

func (w *Watcher) Start() error {
	if err := w.fs.Add(w.root); err != nil {
		return err
	}
	go w.loop()
	return nil
}

func (w *Watcher) Events() <-chan Event {
	return w.events
}

func (w *Watcher) Close() error {
	close(w.done)
	return w.fs.Close()
}

func Overflow() Event {
	return Event{Kind: EventOverflow, Revision: revision(), ResyncRequired: true}
}

func (w *Watcher) loop() {
	for {
		select {
		case ev := <-w.fs.Events:
			rel, _ := filepath.Rel(w.root, ev.Name)
			kind := EventUpdate
			if ev.Has(fsnotify.Create) {
				kind = EventCreate
			}
			if ev.Has(fsnotify.Remove) {
				kind = EventDelete
			}
			if ev.Has(fsnotify.Rename) {
				kind = EventRename
			}
			w.events <- Event{Kind: kind, Path: filepath.ToSlash(rel), Revision: revision()}
		case <-w.fs.Errors:
			w.events <- Overflow()
		case <-w.done:
			return
		}
	}
}

func revision() string {
	return time.Now().UTC().Format("20060102150405.000000000")
}
```

- [ ] **Step 4: Add test helper**

Append to `internal/watch/watch_test.go`:

```go
func waitEvent(t *testing.T, events <-chan Event, path string) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Path == path {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", path)
		}
	}
}
```

- [ ] **Step 5: Run watch tests**

Run:

```bash
gofmt -w internal/watch
go test ./internal/watch -run Test -v
```

Expected: PASS.

- [ ] **Step 6: Add daemon WebSocket event test**

Add the WebSocket dependency:

```bash
go get github.com/gorilla/websocket@v1.5.3
```

Append to `internal/daemon/server_test.go`:

```go
func TestEventsEndpointStreamsWorkspaceChanges(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/events?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	mustWrite(t, filepath.Join(root, "watched.txt"), []byte("hello"))
	var ev watch.Event
	if err := conn.ReadJSON(&ev); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if ev.Path != "watched.txt" {
		t.Fatalf("event %#v", ev)
	}
}
```

Add imports to `internal/daemon/server_test.go`:

```go
"net/url"
"github.com/gorilla/websocket"
"remork/internal/watch"
```

- [ ] **Step 7: Implement daemon `/events` endpoint**

Add in `internal/daemon/server.go` `NewServer`:

```go
s.mux.HandleFunc("/events", s.handleEvents)
```

Append to `internal/daemon/server.go`:

```go
var wsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	watcher, err := watch.New(root)
	if err != nil {
		_ = conn.WriteJSON(watch.Overflow())
		return
	}
	defer watcher.Close()
	if err := watcher.Start(); err != nil {
		_ = conn.WriteJSON(watch.Overflow())
		return
	}
	for ev := range watcher.Events() {
		if err := conn.WriteJSON(ev); err != nil {
			return
		}
	}
}
```

Add imports to `internal/daemon/server.go`:

```go
"github.com/gorilla/websocket"
"remork/internal/watch"
```

- [ ] **Step 8: Run daemon event tests**

Run:

```bash
gofmt -w internal/daemon internal/watch
go test ./internal/watch ./internal/daemon -run 'Test(Watcher|Events)' -v
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum internal/watch internal/daemon
git commit -m "feat: add workspace watch events"
```

## Task 11: CLI Commands For Host Config, Sync, Pull, Status, Diff, Apply, Exec, Shell

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `cmd/remork/main.go`
- Modify: `cmd/remorkd/main.go`

- [ ] **Step 1: Write failing config tests**

Create `internal/config/config_test.go`:

```go
package config

import "testing"

func TestConfigRoundTripHostAndWorkspace(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cfg := Config{Hosts: map[string]Host{"lab": {Name: "lab", URL: "http://10.0.0.12:7731"}}, Workspaces: map[string]Workspace{
		"lab:/data/project": {Host: "lab", RemoteRoot: "/data/project", LocalRoot: "/tmp/project"},
	}}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Hosts["lab"].URL == "" || got.Workspaces["lab:/data/project"].LocalRoot == "" {
		t.Fatalf("bad config: %#v", got)
	}
}

func TestParseWorkspaceRef(t *testing.T) {
	host, root, err := ParseWorkspaceRef("lab:/data/project")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if host != "lab" || root != "/data/project" {
		t.Fatalf("got %s %s", host, root)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/config -run Test -v
```

Expected: FAIL with undefined `NewStore`.

- [ ] **Step 3: Implement config store**

Create `internal/config/config.go`:

```go
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Host struct {
	Name string `json:"name"`
	URL string `json:"url"`
}

type Workspace struct {
	Host string `json:"host"`
	RemoteRoot string `json:"remote_root"`
	LocalRoot string `json:"local_root"`
}

type Config struct {
	Hosts map[string]Host `json:"hosts"`
	Workspaces map[string]Workspace `json:"workspaces"`
}

type Store struct {
	dir string
}

func NewStore(dir string) Store {
	return Store{dir: dir}
}

func (s Store) Save(cfg Config) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, "config.json"), data, 0o644)
}

func (s Store) Load() (Config, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func ParseWorkspaceRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("workspace ref must be host:/absolute/path")
	}
	if !strings.HasPrefix(parts[1], "/") {
		return "", "", errors.New("workspace path must be absolute")
	}
	return parts[0], parts[1], nil
}
```

- [ ] **Step 4: Run config tests**

Run:

```bash
gofmt -w internal/config
go test ./internal/config -run Test -v
```

Expected: PASS.

- [ ] **Step 5: Implement basic CLI wiring**

Add the CLI dependency:

```bash
go get github.com/spf13/cobra@v1.10.1
```

Replace `cmd/remork/main.go` with:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{Use: "remork"}
	root.AddCommand(&cobra.Command{
		Use: "version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("remork " + version)
		},
	})
	root.AddCommand(&cobra.Command{
		Use: "status [workspace]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "workspace %s\n", args[0])
			return nil
		},
	})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Replace `cmd/remorkd/main.go` with:

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"remork/internal/daemon"
)

var version = "dev"

func main() {
	addr := flag.String("addr", "127.0.0.1:7731", "listen address")
	root := flag.String("root", "", "workspace root")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("remorkd " + version)
		return
	}
	if *root == "" {
		log.Fatal("--root is required")
	}
	srv := daemon.NewServer(daemon.Config{Roots: []string{*root}, LargeThreshold: 128 << 20})
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
```

- [ ] **Step 6: Run CLI build tests**

Run:

```bash
gofmt -w cmd/remork cmd/remorkd internal/config
go test ./internal/config -run Test -v
go build ./cmd/remork ./cmd/remorkd
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum cmd internal/config
git commit -m "feat: add remork command skeleton and config"
```

## Task 12: End-To-End MVP Tests

**Files:**
- Create: `test/e2e/remork_e2e_test.go`
- Modify: implementation files touched by failures

- [ ] **Step 1: Write e2e tests for sync/pull/apply/exec/watch**

Create `test/e2e/remork_e2e_test.go`:

```go
package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/apply"
	"remork/internal/client"
	"remork/internal/daemon"
	"remork/internal/manifest"
	"remork/internal/planner"
	"remork/internal/state"
	"remork/internal/transfer"
)

func TestSyncPullApplyExecWorkflow(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWrite(t, filepath.Join(remote, "src", "main.txt"), []byte("hello"))
	mustWrite(t, filepath.Join(remote, "big.tar.gz"), []byte("0123456789"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 5}).Handler())
	defer srv.Close()
	c := client.New(srv.URL)

	man, err := c.Manifest(remote, ".")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	plan := planner.PlanSync(man, state.Snapshot{}, planner.Options{WorkspaceRef: "lab:" + remote})
	for _, op := range plan.Operations {
		switch op.Kind {
		case planner.OpDownload:
			data, err := c.Download(remote, op.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := transfer.WriteFile(local, op.Path, data); err != nil {
				t.Fatal(err)
			}
		case planner.OpWriteMeta:
			meta := manifest.BuildLargeMeta("lab:"+remote, op.Entry)
			if err := transfer.WriteLargeMeta(local, op.Path, meta); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(local, "big.tar.gz.meta")); err != nil {
		t.Fatalf("missing meta: %v", err)
	}

	base := state.HashBytes([]byte("hello"))
	if err := os.WriteFile(filepath.Join(local, "src", "main.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := c.Apply(remote, apply.Changeset{Changes: []apply.Change{
		{Path: "src/main.txt", Kind: apply.ChangeUpdate, BaseHash: base, Content: []byte("hello world")},
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied {
		t.Fatalf("not applied: %#v", res)
	}
	got, _ := os.ReadFile(filepath.Join(remote, "src", "main.txt"))
	if string(got) != "hello world" {
		t.Fatalf("remote not updated: %q", got)
	}
}
```

- [ ] **Step 2: Run e2e test to verify current gaps**

Run:

```bash
go test ./test/e2e -run TestSyncPullApplyExecWorkflow -v
```

Expected: PASS. A failure here means one of the earlier package-level tests missed an integration boundary; return to the failing package from the stack trace, add a focused regression test there, make it pass, then rerun this e2e test.

- [ ] **Step 3: Add edge-case regression tests**

Append to `test/e2e/remork_e2e_test.go`:

```go
func TestApplyConflictDoesNotOverwriteRemote(t *testing.T) {
	remote := t.TempDir()
	mustWrite(t, filepath.Join(remote, "a.txt"), []byte("remote change"))
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	defer srv.Close()
	c := client.New(srv.URL)
	res, err := c.Apply(remote, apply.Changeset{Changes: []apply.Change{
		{Path: "a.txt", Kind: apply.ChangeUpdate, BaseHash: state.HashBytes([]byte("base")), Content: []byte("local")},
	}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if res.Applied {
		t.Fatal("conflict must not apply")
	}
	got, _ := os.ReadFile(filepath.Join(remote, "a.txt"))
	if string(got) != "remote change" {
		t.Fatalf("remote overwritten: %q", got)
	}
}
```

Append helper:

```go
func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 4: Run full tests**

Run:

```bash
gofmt -w test/e2e
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add test internal cmd
git commit -m "test: add remork end-to-end workflow coverage"
```

## Task 13: Edge-Case Hardening Checklist

**Files:**
- Modify: `internal/daemon/server_test.go`
- Modify: `internal/apply/apply_test.go`
- Modify: `internal/planner/planner_test.go`
- Modify: `internal/watch/watch_test.go`
- Modify: `internal/exec/exec_test.go`
- Modify: `internal/pty/session_test.go`

- [ ] **Step 1: Add daemon path edge test**

Append to `internal/daemon/server_test.go`:

```go
func TestDownloadParentTraversalReturnsBadRequest(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/download?root=" + root + "&path=../escape")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Add apply conflict edge tests**

Append to `internal/apply/apply_test.go`:

```go
func TestApplyCreateConflictsWhenRemoteExists(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "exists.txt"), []byte("remote"))
	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "exists.txt", Kind: ChangeCreate, Content: []byte("local")},
	}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Applied || len(result.Conflicts) != 1 {
		t.Fatalf("bad result: %#v", result)
	}
}

func TestApplyDeleteConflictsWhenRemoteChanged(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("remote change"))
	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "a.txt", Kind: ChangeDelete, BaseHash: state.HashBytes([]byte("base"))},
	}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Applied {
		t.Fatal("delete conflict must not apply")
	}
}
```

- [ ] **Step 3: Add planner edge tests**

Append to `internal/planner/planner_test.go`:

```go
func TestPullLargePolicySwitchesBetweenMetaAndDownload(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "big.bin", Type: api.FileTypeFile, Large: true, Size: 200, Revision: "rev-big"},
	}}
	metaPlan := PlanPull(manifest, state.Snapshot{}, Options{TargetPath: "big.bin"})
	assertOp(t, metaPlan, "big.bin", OpWriteMeta)
	downloadPlan := PlanPull(manifest, state.Snapshot{}, Options{TargetPath: "big.bin", IncludeLarge: true})
	assertOp(t, downloadPlan, "big.bin", OpDownload)
}
```

- [ ] **Step 4: Add watch, exec, and PTY edge tests**

Append to `internal/watch/watch_test.go`:

```go
func TestOverflowRequiresManifestReconcile(t *testing.T) {
	ev := Overflow()
	if ev.Kind != EventOverflow {
		t.Fatalf("kind %s", ev.Kind)
	}
	if !ev.ResyncRequired {
		t.Fatal("overflow must require reconcile")
	}
}
```

Append to `internal/exec/exec_test.go`:

```go
func TestRunEmptyCommandFails(t *testing.T) {
	res, err := Run(Options{})
	if err == nil {
		t.Fatal("expected empty command error")
	}
	if res.ExitCode != -1 {
		t.Fatalf("exit code %d", res.ExitCode)
	}
}
```

Append to `internal/pty/session_test.go`:

```go
func TestCloseUnknownSessionIsNoop(t *testing.T) {
	m := NewManager(time.Second)
	if err := m.Close("missing"); err != nil {
		t.Fatalf("close missing: %v", err)
	}
}
```

- [ ] **Step 5: Run focused package tests**

Run:

```bash
go test ./internal/paths ./internal/manifest ./internal/apply ./internal/planner ./internal/watch ./internal/exec ./internal/pty -run Test -v
```

Expected: PASS.

- [ ] **Step 6: Run integration tests repeatedly**

Run:

```bash
go test ./test/e2e -count=5 -run Test -v
```

Expected: PASS five times to catch flaky watcher or PTY behavior.

- [ ] **Step 7: Run race detector for non-PTY packages**

Run:

```bash
go test -race ./internal/daemon ./internal/client ./internal/state ./internal/watch ./test/e2e
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal test
git commit -m "test: harden remork edge cases"
```

## Task 14: Final Verification And Manual Smoke Test

**Files:**
- Modify: `docs/superpowers/specs/2026-04-30-remote-workspace-agent-design.md` only if behavior changed during implementation

- [ ] **Step 1: Run full automated verification**

Run:

```bash
go test ./...
go build ./cmd/remork ./cmd/remorkd
scripts/build-release.sh dev
```

Expected: PASS. `dist/` contains `remork`, four `remorkd-*` target binaries, `remorkd.example.toml`, and `checksums.txt`.

- [ ] **Step 2: Verify local-platform release daemon does not need Go at runtime**

Run:

```bash
local_daemon="dist/remorkd-$(go env GOOS)-$(go env GOARCH)"
"$local_daemon" --version
```

Expected:

```text
remorkd dev
```

This confirms the copied daemon binary can start directly. Remote servers do not run `go build`, `go get`, `brew`, or any internet-dependent install step.

- [ ] **Step 3: Manual local daemon smoke test**

Run:

```bash
local_daemon="dist/remorkd-$(go env GOOS)-$(go env GOARCH)"
tmp_remote=$(mktemp -d)
tmp_local=$(mktemp -d)
printf 'hello\n' > "$tmp_remote/a.txt"
"$local_daemon" --root "$tmp_remote" --addr 127.0.0.1:7731 &
daemon_pid=$!
sleep 1
curl -fsS "http://127.0.0.1:7731/manifest?root=$tmp_remote&path=.&recursive=true"
kill "$daemon_pid"
rm -rf "$tmp_remote" "$tmp_local"
```

Expected: manifest JSON contains `a.txt`.

- [ ] **Step 4: Inspect git state**

Run:

```bash
git status --short
```

Expected: clean, unless documentation was intentionally updated.

- [ ] **Step 5: Commit final docs if changed**

If docs changed:

```bash
git add docs
git commit -m "docs: align remork spec with implementation"
```

## Task 15: Real Remote Offline Daemon Validation

**Files:**
- Modify: `docs/superpowers/specs/2026-04-30-remote-workspace-agent-design.md` only if behavior changed during remote validation
- Modify: `docs/superpowers/plans/2026-04-30-remork-mvp-implementation-plan.md` only if validation commands need correction

- [ ] **Step 1: Confirm SSH aliases still resolve**

Run:

```bash
ssh -G z00879328_docker | awk '/^(hostname|user|port|identityfile) / {print}'
ssh -G z00879328_docker_2.6 | awk '/^(hostname|user|port|identityfile) / {print}'
```

Expected:

```text
hostname 175.100.2.7
user root
port 22022
identityfile ~/.ssh/id_ed25519
hostname 175.100.2.6
user root
port 2226
identityfile ~/.ssh/id_ed25519
```

- [ ] **Step 2: Confirm remote platforms without changing files**

Run:

```bash
ssh -o BatchMode=yes -o ConnectTimeout=8 z00879328_docker 'printf "user=%s\n" "$(whoami)"; printf "os=%s\n" "$(uname -s)"; printf "arch=%s\n" "$(uname -m)"'
ssh -o BatchMode=yes -o ConnectTimeout=8 z00879328_docker_2.6 'printf "user=%s\n" "$(whoami)"; printf "os=%s\n" "$(uname -s)"; printf "arch=%s\n" "$(uname -m)"'
```

Expected for both hosts:

```text
user=root
os=Linux
arch=aarch64
```

- [ ] **Step 3: Build the offline Linux arm64 daemon locally**

Run:

```bash
scripts/build-release.sh dev
test -x dist/remorkd-linux-arm64
shasum -a 256 dist/remorkd-linux-arm64
```

Expected: all commands pass locally. Do not build on the remote hosts.

- [ ] **Step 4: Copy binary and create isolated remote test workspaces**

Run:

```bash
for host in z00879328_docker z00879328_docker_2.6; do
  scp dist/remorkd-linux-arm64 "$host:/tmp/remorkd"
  ssh "$host" 'chmod +x /tmp/remorkd && rm -rf /tmp/remork-e2e && mkdir -p /tmp/remork-e2e && printf "hello\n" > /tmp/remork-e2e/a.txt && /tmp/remorkd --version'
done
```

Expected: each host prints `remorkd dev`. No compiler, package manager, or internet access is used remotely.

- [ ] **Step 5: Start daemon on each remote and verify locally on the remote host**

Run:

```bash
for host in z00879328_docker z00879328_docker_2.6; do
  ssh "$host" 'if [ -f /tmp/remorkd.pid ]; then kill "$(cat /tmp/remorkd.pid)" 2>/dev/null || true; fi; nohup /tmp/remorkd --root /tmp/remork-e2e --addr 0.0.0.0:17731 >/tmp/remorkd.log 2>&1 & echo $! > /tmp/remorkd.pid; sleep 1; curl -fsS "http://127.0.0.1:17731/manifest?root=/tmp/remork-e2e&path=.&recursive=true"'
done
```

Expected: each remote-side curl returns manifest JSON containing `a.txt`. If this passes but local direct curl fails, the issue is VPN/firewall exposure rather than daemon startup.

- [ ] **Step 6: Verify direct VPN HTTP access from local machine**

Run:

```bash
curl -fsS 'http://175.100.2.7:17731/manifest?root=/tmp/remork-e2e&path=.&recursive=true'
curl -fsS 'http://175.100.2.6:17731/manifest?root=/tmp/remork-e2e&path=.&recursive=true'
```

Expected: both local curls return manifest JSON containing `a.txt`. This validates the intended daemon transport path without SSH tunnels.

- [ ] **Step 7: Cleanup remote test processes and files**

Run:

```bash
for host in z00879328_docker z00879328_docker_2.6; do
  ssh "$host" 'if [ -f /tmp/remorkd.pid ]; then kill "$(cat /tmp/remorkd.pid)" 2>/dev/null || true; fi; rm -f /tmp/remorkd.pid /tmp/remorkd.log /tmp/remorkd; rm -rf /tmp/remork-e2e'
done
```

Expected: cleanup succeeds on both hosts.

- [ ] **Step 8: Record any environment-specific findings**

If validation reveals a real host constraint, update the plan or spec with a concrete note and commit it:

```bash
git add docs
git commit -m "docs: record remork remote validation findings"
```

## Self-Review

Spec coverage:

- Local editable working copy: Task 3, Task 7, Task 11, Task 12.
- Remote-to-local sync and pull: Task 2, Task 4, Task 5, Task 6, Task 12.
- Large file `.meta` placeholders and `128MB` default: Task 2, Task 4, Task 6, Task 13.
- Explicit write-back with base verification: Task 7, Task 12, Task 13.
- Remote command execution: Task 8, Task 13.
- Interactive PTY shell: Task 9, Task 13.
- Watch/events with manifest fallback: Task 10, Task 13.
- Offline remote deployment with prebuilt daemon binary: Task 0, Task 14, Task 15.
- No target Git pollution: Task 2 skips `.git`, Task 3 skips `.git`, Task 13 adds regression coverage.
- Edge cases and TDD: every implementation task starts with failing tests, then minimal code, then verification.

Known follow-up outside this MVP:

- Production-grade CLI UX for all commands beyond the tested skeleton.
- Background local service.
- mTLS and multi-user policy.
- Binary diff.
- Rich conflict resolution UI.
