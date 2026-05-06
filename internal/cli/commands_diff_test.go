package cli

import (
	"path/filepath"
	"testing"

	"remork/internal/state"
	"remork/internal/workspace"
)

func TestDiffNoChangesLeavesStdoutEmpty(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	stateDir := filepath.Join(home, ".remork", "state", "ws-test")
	binding := workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/remote/root",
		WorkspaceID: "ws-test",
		StateDir:    stateDir,
	}
	if err := workspace.WriteBinding(local, binding); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	if err := state.NewStore(stateDir).Save(state.Snapshot{
		WorkspaceRef: "lab:/remote/root",
		Entries:      map[string]state.TrackedFile{},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "diff")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("empty diff should not write stdout, got %q", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("empty diff should not write stderr, got %q", stderr.String())
	}
}
