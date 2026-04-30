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

func TestOverflowRequiresManifestReconcile(t *testing.T) {
	ev := Overflow()
	if ev.Kind != EventOverflow {
		t.Fatalf("kind %s", ev.Kind)
	}
	if !ev.ResyncRequired {
		t.Fatal("overflow must require reconcile")
	}
}

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
