package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJSONLStorePersistsAndFiltersEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.jsonl")
	store := NewJSONLStore(path)
	entry := Entry{
		ID:           "op-test",
		StartedAt:    time.Unix(1, 0).UTC(),
		FinishedAt:   time.Unix(2, 0).UTC(),
		ClientID:     "tao-macbook",
		Root:         "/workspace",
		Operation:    "apply",
		Result:       "success",
		StatusCode:   200,
		ChangedPaths: []string{"a.txt"},
	}
	if err := store.Append(entry); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file: %v", err)
	}
	got, err := store.List(Filter{Root: "/workspace", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "op-test" || got[0].ClientID != "tao-macbook" {
		t.Fatalf("bad entries: %#v", got)
	}
	other, err := store.List(Filter{Root: "/other", Limit: 10})
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("unexpected entries: %#v", other)
	}
}

func TestJSONLStoreListsLargeEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.jsonl")
	store := NewJSONLStore(path)
	entry := Entry{
		ID:             "op-large",
		Root:           "/workspace",
		Operation:      "apply",
		RequestSummary: map[string]any{"paths": strings.Repeat("a", 80<<10)},
		Result:         "success",
		StatusCode:     200,
	}
	if err := store.Append(entry); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := store.List(Filter{Root: "/workspace"})
	if err != nil {
		t.Fatalf("list should handle large JSONL entries: %v", err)
	}
	if len(got) != 1 || got[0].ID != "op-large" {
		t.Fatalf("entries = %#v", got)
	}
}

func TestJSONLStoreRejectsSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".remork")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store := NewJSONLStore(filepath.Join(root, ".remork", "log", "operations.jsonl"))

	if err := store.Append(Entry{ID: "op-symlink-parent"}); err == nil {
		t.Fatal("expected symlink parent error")
	}
	if _, err := os.Stat(filepath.Join(outside, "log", "operations.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("outside log should not be written: %v", err)
	}
}

func TestJSONLStoreRejectsSymlinkLogFile(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	logDir := filepath.Join(root, ".remork", "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(logDir, "operations.jsonl")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store := NewJSONLStore(filepath.Join(logDir, "operations.jsonl"))

	if err := store.Append(Entry{ID: "op-symlink-file"}); err == nil {
		t.Fatal("expected symlink log file error")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside log should not be written: %v", err)
	}
}

func TestJSONLStoreListMissingLogReturnsEmpty(t *testing.T) {
	store := NewJSONLStore(filepath.Join(t.TempDir(), ".remork", "log", "operations.jsonl"))
	got, err := store.List(Filter{})
	if err != nil {
		t.Fatalf("list missing log: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("entries = %#v, want empty", got)
	}
}

func TestMemoryStoreLimitReturnsMostRecentEntries(t *testing.T) {
	store := NewMemoryStore()
	for _, id := range []string{"op-1", "op-2", "op-3"} {
		if err := store.Append(Entry{ID: id, Root: "/workspace"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := store.List(Filter{Root: "/workspace", Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0].ID != "op-2" || got[1].ID != "op-3" {
		t.Fatalf("bad entries: %#v", got)
	}
}
