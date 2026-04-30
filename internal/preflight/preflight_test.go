package preflight

import (
	"strings"
	"testing"

	"remork/internal/exitcode"
)

func TestRunPreflightBlocksDirtyWorkspace(t *testing.T) {
	decision := Decide(WorkspaceState{LocalDirty: 1, RemoteStale: false}, Options{})
	if decision.Allow {
		t.Fatalf("expected blocked decision: %#v", decision)
	}
	if decision.ExitCode != exitcode.LocalDirtyBlocked {
		t.Fatalf("exit = %d", decision.ExitCode)
	}
	if !strings.Contains(decision.Message, "remork apply") {
		t.Fatalf("message = %q", decision.Message)
	}
}

func TestRunPreflightAllowsRemoteOnly(t *testing.T) {
	decision := Decide(WorkspaceState{LocalDirty: 1, RemoteStale: true}, Options{RemoteOnly: true})
	if !decision.Allow {
		t.Fatalf("expected allow: %#v", decision)
	}
	if !strings.Contains(decision.Warning, "local pending changes are ignored") {
		t.Fatalf("warning = %q", decision.Warning)
	}
}
