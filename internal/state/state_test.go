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

func TestBasePathNestedPath(t *testing.T) {
	stateDir := t.TempDir()
	got, err := BasePath(stateDir, "src/main.txt")
	if err != nil {
		t.Fatalf("BasePath: %v", err)
	}
	want := filepath.Join(stateDir, "base", "src", "main.txt")
	if got != want {
		t.Fatalf("BasePath = %q, want %q", got, want)
	}
}

func TestBasePathRejectsEscape(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := BasePath(stateDir, "../escape.txt"); err == nil {
		t.Fatal("BasePath accepted path escape, want error")
	}
}

func TestBasePathRejectsSymlinkBaseRoot(t *testing.T) {
	stateDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stateDir, "base")); err != nil {
		t.Fatalf("symlink base root: %v", err)
	}

	if _, err := BasePath(stateDir, "a.txt"); err == nil {
		t.Fatal("BasePath accepted symlink base root, want error")
	}
	if _, err := os.Lstat(filepath.Join(outside, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside path was touched: %v", err)
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

func TestDetectDirtyReportsTrackedFileReplacedByDirectory(t *testing.T) {
	local := t.TempDir()
	if err := os.Mkdir(filepath.Join(local, "tracked.txt"), 0o755); err != nil {
		t.Fatalf("mkdir tracked path: %v", err)
	}
	mustWrite(t, filepath.Join(local, "tracked.txt", "child.txt"), []byte("child"))

	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"tracked.txt": {Path: "tracked.txt", BaseHash: HashBytes([]byte("before")), Type: api.FileTypeFile},
	}}
	dirty, err := DetectDirty(local, snap)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	assertChange(t, dirty, "tracked.txt", ChangeModify)
}

func TestDetectDirtyRejectsTrackedPathEscape(t *testing.T) {
	parent := t.TempDir()
	local := filepath.Join(parent, "local")
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	outside := filepath.Join(parent, "outside.txt")
	mustWrite(t, outside, []byte("outside"))

	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"../outside.txt": {Path: "../outside.txt", BaseHash: HashBytes([]byte("outside")), Type: api.FileTypeFile},
	}}
	if _, err := DetectDirty(local, snap); err == nil {
		t.Fatal("DetectDirty accepted tracked path escape, want error")
	}
}

func TestDetectDirtyRejectsTrackedSymlinkFile(t *testing.T) {
	local := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, []byte("outside"))
	if err := os.Symlink(outside, filepath.Join(local, "a.txt")); err != nil {
		t.Fatalf("symlink local file: %v", err)
	}

	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"a.txt": {Path: "a.txt", BaseHash: HashBytes([]byte("outside")), Type: api.FileTypeFile},
	}}
	if _, err := DetectDirty(local, snap); err == nil {
		t.Fatal("DetectDirty accepted tracked symlink file, want error")
	}
}

func TestDetectDirtyRejectsLargeTrackedPathEscape(t *testing.T) {
	parent := t.TempDir()
	local := filepath.Join(parent, "local")
	if err := os.Mkdir(local, 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}

	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"../outside.bin": {Path: "../outside.bin", Large: true, MetaPath: "../outside.bin.meta", Type: api.FileTypeFile},
	}}
	if _, err := DetectDirty(local, snap); err == nil {
		t.Fatal("DetectDirty accepted large tracked path escape, want error")
	}
}

func TestDetectDirtyRejectsLargeTrackedSymlinkFile(t *testing.T) {
	local := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.bin")
	mustWrite(t, outside, []byte("outside"))
	if err := os.Symlink(outside, filepath.Join(local, "large.bin")); err != nil {
		t.Fatalf("symlink local file: %v", err)
	}

	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"large.bin": {Path: "large.bin", Large: true, MetaPath: "large.bin.meta", Type: api.FileTypeFile},
	}}
	if _, err := DetectDirty(local, snap); err == nil {
		t.Fatal("DetectDirty accepted large tracked symlink file, want error")
	}
}

func TestDetectDirtyIgnoresLocalBindingMarker(t *testing.T) {
	local := t.TempDir()
	mustWrite(t, filepath.Join(local, ".remork-local.json"), []byte(`{"host":"lab"}`))

	dirty, err := DetectDirty(local, Snapshot{Entries: map[string]TrackedFile{}})
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	if len(dirty) != 0 {
		t.Fatalf("binding marker must be ignored: %#v", dirty)
	}
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

func TestDetectDirtyLargePlaceholderReportsMaterializedFile(t *testing.T) {
	local := t.TempDir()
	mustWrite(t, filepath.Join(local, "large.bin"), []byte("local materialized content"))
	mustWrite(t, filepath.Join(local, "large.bin.meta"), []byte("{}"))
	snap := Snapshot{WorkspaceRef: "lab:/workspace", Entries: map[string]TrackedFile{
		"large.bin": {Path: "large.bin", Large: true, MetaPath: "large.bin.meta", Type: api.FileTypeFile},
	}}

	dirty, err := DetectDirty(local, snap)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	assertChange(t, dirty, "large.bin", ChangeModify)
}

func TestDirtyDetectionSkipsProjectGitDirectory(t *testing.T) {
	local := t.TempDir()
	mustWrite(t, filepath.Join(local, ".git", "config"), []byte("ignored"))
	dirty, err := DetectDirty(local, Snapshot{Entries: map[string]TrackedFile{}})
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	if len(dirty) != 0 {
		t.Fatalf("git internals must be ignored: %#v", dirty)
	}
}

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

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertChange(t *testing.T, changes []DirtyChange, path string, kind ChangeKind) {
	t.Helper()
	for _, c := range changes {
		if c.Path == path && c.Kind == kind {
			return
		}
	}
	t.Fatalf("missing change %s %s in %#v", kind, path, changes)
}
