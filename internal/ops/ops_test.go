package ops

import (
	"os"
	"path/filepath"
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
