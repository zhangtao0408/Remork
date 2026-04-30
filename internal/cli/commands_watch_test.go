package cli

import (
	"testing"

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
