package manifest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"remork/internal/api"
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

func TestScanSkipsDotRemorkDirectory(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".remork", "state.json"), []byte("private"))
	mustWrite(t, filepath.Join(root, "src", "main.go"), []byte("package main"))

	got, err := Scan(root, ".", Options{LargeThreshold: 128 << 20})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, e := range got.Entries {
		if e.Path == ".remork/state.json" {
			t.Fatal("manifest must not include remork internals")
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

func TestScanHandlesEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	got, err := Scan(root, ".", Options{LargeThreshold: 128 << 20})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("entries %#v", got.Entries)
	}
	if got.Revision == "" {
		t.Fatal("missing revision")
	}
}

func TestLargeThresholdBoundary(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "exact.bin"), bytes.Repeat([]byte("x"), 8))
	mustWrite(t, filepath.Join(root, "above.bin"), bytes.Repeat([]byte("x"), 9))
	got, err := Scan(root, ".", Options{LargeThreshold: 8})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if findEntry(t, got.Entries, "exact.bin").Large {
		t.Fatal("file at threshold should sync normally")
	}
	if !findEntry(t, got.Entries, "above.bin").Large {
		t.Fatal("file above threshold should be large")
	}
}

func TestRevisionIncludesNanosecondMTime(t *testing.T) {
	a := fakeFileInfo{size: 128 << 20, modTime: time.Unix(100, 100).UTC()}
	b := fakeFileInfo{size: 128 << 20, modTime: time.Unix(100, 200).UTC()}
	if revisionFor(a) == revisionFor(b) {
		t.Fatal("same-size files with different nanosecond mtimes need different revisions")
	}
}

type fakeFileInfo struct {
	size    int64
	modTime time.Time
}

func (f fakeFileInfo) Name() string       { return "large.bin" }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o644 }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

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
