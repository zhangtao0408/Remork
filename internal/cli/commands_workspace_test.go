package cli

import (
	"os"
	"path/filepath"
	"testing"

	"remork/internal/config"
	"remork/internal/workspace"
)

func TestWorkspaceCommandShowsCurrentBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	stateDir := filepath.Join(home, ".remork", "state", "ws_test")
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws_test",
		StateDir:    stateDir,
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err := executeCommand(cmd, "workspace")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	mustContain(t, out.String(), "local:")
	mustContain(t, out.String(), local)
	mustContain(t, out.String(), "host: lab")
	mustContain(t, out.String(), "remote_root: /data/project")
	mustContain(t, out.String(), "workspace_id: ws_test")
	mustNotContain(t, out.String(), "token")
}

func TestWorkspaceRemoveDeletesLocalBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws_test",
		StateDir:    filepath.Join(home, ".remork", "state", "ws_test"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err := executeCommand(cmd, "workspace", "remove")
	if err != nil {
		t.Fatalf("workspace remove: %v", err)
	}
	mustContain(t, out.String(), "removed workspace binding")
	if _, err := os.Stat(filepath.Join(local, workspace.MarkerName)); !os.IsNotExist(err) {
		t.Fatalf("binding marker still exists: %v", err)
	}
	if _, _, err := workspace.ResolveFrom(local); err == nil {
		t.Fatal("binding should no longer resolve")
	}
}

func TestHostListAndRemove(t *testing.T) {
	home := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab-a": {Name: "lab-a", URL: "http://127.0.0.1:18101", NoProxy: true},
		"lab-b": {Name: "lab-b", URL: "http://127.0.0.1:18102", TokenEnv: "REMORK_TOKEN"},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err := executeCommand(cmd, "host", "list")
	if err != nil {
		t.Fatalf("host list: %v", err)
	}
	mustContain(t, out.String(), "lab-a")
	mustContain(t, out.String(), "no_proxy")
	mustContain(t, out.String(), "lab-b")
	mustContain(t, out.String(), "REMORK_TOKEN")

	cmd = NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err = executeCommand(cmd, "host", "remove", "lab-a")
	if err != nil {
		t.Fatalf("host remove: %v", err)
	}
	mustContain(t, out.String(), "removed host lab-a")
	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := cfg.Hosts["lab-a"]; ok {
		t.Fatal("lab-a should be removed")
	}
	if _, ok := cfg.Hosts["lab-b"]; !ok {
		t.Fatal("lab-b should remain")
	}
}

func TestHostRemoveMissingFails(t *testing.T) {
	home := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	if _, err := executeCommand(cmd, "host", "remove", "missing"); err == nil {
		t.Fatal("host remove should fail for missing host")
	}
}
