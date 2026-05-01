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

func TestWatcherEmitsNestedFileUpdate(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "main.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := New(root)
	if err != nil {
		t.Fatalf("watcher: %v", err)
	}
	defer w.Close()
	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, w.Events(), "src/main.txt")
	if ev.Path != "src/main.txt" {
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

func TestWatcherEmitDoesNotBlockWhenBufferIsFull(t *testing.T) {
	w := &Watcher{events: make(chan Event, 1), done: make(chan struct{})}
	w.events <- Event{Kind: EventUpdate, Path: "queued.txt", Revision: revision()}

	done := make(chan bool, 1)
	go func() {
		done <- w.emit(Event{Kind: EventUpdate, Path: "dropped.txt", Revision: revision()})
	}()

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("emit returned false before watcher was closed")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("emit blocked with a full event buffer")
	}
}

func TestWatcherEmitStopsAfterClose(t *testing.T) {
	w := &Watcher{events: make(chan Event), done: make(chan struct{})}
	close(w.done)

	done := make(chan bool, 1)
	go func() {
		done <- w.emit(Event{Kind: EventUpdate, Path: "closed.txt", Revision: revision()})
	}()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("emit returned true after watcher was closed")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("emit blocked after watcher was closed")
	}
}

func TestWatcherEmitsDelete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gone.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := New(root)
	if err != nil {
		t.Fatalf("watcher: %v", err)
	}
	defer w.Close()
	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, w.Events(), "gone.txt")
	if ev.Kind != EventDelete {
		t.Fatalf("kind %s", ev.Kind)
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
