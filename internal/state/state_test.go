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
