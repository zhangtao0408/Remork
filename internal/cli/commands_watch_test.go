package cli

import (
	"testing"
	"time"

	"remork/internal/watch"
)

func TestWatchNormalEventDoesNotReconcile(t *testing.T) {
	ev := watch.Event{Kind: watch.EventUpdate, Path: "a.txt", Revision: "event-rev"}
	if needsWatchReconcile(ev) {
		t.Fatalf("normal event should not force reconcile: %#v", ev)
	}
}

func TestWatchOverflowReconciles(t *testing.T) {
	ev := watch.Event{Kind: watch.EventOverflow, ResyncRequired: true}
	if !needsWatchReconcile(ev) {
		t.Fatalf("overflow event should force reconcile: %#v", ev)
	}
}

func TestWatchSyncTarget(t *testing.T) {
	tests := []struct {
		name string
		ev   watch.Event
		want string
	}{
		{
			name: "update returns path",
			ev:   watch.Event{Kind: watch.EventUpdate, Path: "src/main.txt"},
			want: "src/main.txt",
		},
		{
			name: "delete returns full reconcile target",
			ev:   watch.Event{Kind: watch.EventDelete, Path: "src/main.txt"},
			want: "",
		},
		{
			name: "rename returns full reconcile target",
			ev:   watch.Event{Kind: watch.EventRename, Path: "src/new.txt"},
			want: "",
		},
		{
			name: "overflow returns full reconcile target",
			ev:   watch.Event{Kind: watch.EventOverflow, ResyncRequired: true},
			want: "",
		},
		{
			name: "resync returns full reconcile target",
			ev:   watch.Event{Kind: watch.EventUpdate, Path: "src/main.txt", ResyncRequired: true},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := watchSyncTarget(tt.ev); got != tt.want {
				t.Fatalf("watchSyncTarget(%#v) = %q, want %q", tt.ev, got, tt.want)
			}
		})
	}
}

func TestDefaultWatchOptions(t *testing.T) {
	opts := defaultWatchOptions()
	if opts.ReconcileInterval <= 0 {
		t.Fatalf("ReconcileInterval = %s, want positive", opts.ReconcileInterval)
	}
	if opts.Debounce <= 0 {
		t.Fatalf("Debounce = %s, want positive", opts.Debounce)
	}
	if opts.ReconcileInterval < time.Second {
		t.Fatalf("ReconcileInterval = %s, want practical default", opts.ReconcileInterval)
	}
	if opts.Debounce > time.Second {
		t.Fatalf("Debounce = %s, want sub-second default", opts.Debounce)
	}
}
