package cli

import (
	"path/filepath"
	"testing"

	"remork/internal/config"
	"remork/internal/workspace"
)

func TestConnectCommandIsRegistered(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	found, _, err := cmd.Find([]string{"connect"})
	if err != nil || found == nil {
		t.Fatalf("connect command not registered: %v", err)
	}
}

func TestConnectCommandNonInteractiveBindsWorkspace(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	probe := &connectProbe{roots: []string{"/home/me"}}
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local, DaemonProbe: probe})
	out, err := executeCommand(cmd, "connect", "--url", "http://lab.example.internal:17731", "--host", "lab", "--workspace-path", "project-a", "--token", "secret", "--first-sync=false", "--non-interactive")
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, out.String())
	}
	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosts["lab"].TokenFile == "" {
		t.Fatal("connect should save a token file")
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatal(err)
	}
	if binding.RemoteRoot != "/home/me/project-a" {
		t.Fatalf("remote root = %q", binding.RemoteRoot)
	}
	mustContain(t, out.String(), "connected")
}

func TestConnectCommandNonInteractiveRequiresURL(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir(), DaemonProbe: &connectProbe{roots: []string{"/data"}}})
	_, err := executeCommand(cmd, "connect", "--non-interactive")
	if err == nil {
		t.Fatal("connect without URL should fail")
	}
}
