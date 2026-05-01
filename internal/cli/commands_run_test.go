package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"remork/internal/config"
	execx "remork/internal/exec"
	"remork/internal/workspace"
)

func TestRunCommandWarnsWhenOutputIsTruncated(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	stateDir := filepath.Join(home, ".remork", "state", "ws_test")
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/remote/root",
		WorkspaceID: "ws_test",
		StateDir:    stateDir,
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(execx.Result{
			Stdout:          "partial stdout\n",
			Stderr:          "partial stderr\n",
			StdoutTruncated: true,
			StderrTruncated: true,
		})
	}))
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab": {Name: "lab", URL: server.URL},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err := executeCommand(cmd, "run", "--no-sync-check", "echo ignored")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	mustContain(t, out.String(), "partial stdout")
	mustContain(t, out.String(), "partial stderr")
	mustContain(t, out.String(), "stdout truncated")
	mustContain(t, out.String(), "stderr truncated")
}
