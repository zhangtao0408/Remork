package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndResolveBindingFromCurrentDirectory(t *testing.T) {
	local := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state", "ws_123")
	binding := Binding{
		Version:     1,
		Host:        "lab-a",
		RemoteRoot:  "/data/project-a",
		WorkspaceID: "ws_123",
		StateDir:    stateDir,
	}
	if err := WriteBinding(local, binding); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	nested := filepath.Join(local, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	got, root, err := ResolveFrom(nested)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if root != local {
		t.Fatalf("root %q, want %q", root, local)
	}
	if got.Host != binding.Host || got.RemoteRoot != binding.RemoteRoot || got.WorkspaceID != binding.WorkspaceID || got.StateDir != binding.StateDir {
		t.Fatalf("binding %#v, want %#v", got, binding)
	}
}

func TestBindingRejectsSecrets(t *testing.T) {
	err := WriteBinding(t.TempDir(), Binding{Version: 1, Host: "lab-a", RemoteRoot: "/data/project-a", Token: "secret"})
	if err == nil {
		t.Fatal("WriteBinding should reject token secrets")
	}
}
