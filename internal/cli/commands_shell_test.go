package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/workspace"
)

func TestShellListDoesNotRunStatusOrPreflight(t *testing.T) {
	home, local := shellCommandWorkspace(t)
	var statusRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			statusRequests++
			http.Error(w, "status should not be called", http.StatusTeapot)
		case "/shell/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"sessions": []api.ShellSessionInfo{
				{ID: "sess-1", Command: []string{"sh"}, LastActive: "2026-05-02T00:00:00Z"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	writeShellCommandConfig(t, home, local, server.URL)

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "shell", "--list")
	if err != nil {
		t.Fatalf("shell --list: %v\noutput:\n%s", err, out.String())
	}
	mustContain(t, out.String(), "sess-1")
	mustContain(t, out.String(), "sh")
	if statusRequests != 0 {
		t.Fatalf("status requests = %d, want 0", statusRequests)
	}
}

func TestShellKillDoesNotRunStatusOrPreflight(t *testing.T) {
	home, local := shellCommandWorkspace(t)
	var statusRequests int
	var killedID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			statusRequests++
			http.Error(w, "status should not be called", http.StatusTeapot)
		case "/shell/sessions":
			if r.Method != http.MethodDelete {
				t.Fatalf("method = %s, want DELETE", r.Method)
			}
			killedID = r.URL.Query().Get("id")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	writeShellCommandConfig(t, home, local, server.URL)

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "shell", "--kill", "sess-1")
	if err != nil {
		t.Fatalf("shell --kill: %v\noutput:\n%s", err, out.String())
	}
	mustContain(t, out.String(), "killed sess-1")
	if killedID != "sess-1" {
		t.Fatalf("killed id = %q, want sess-1", killedID)
	}
	if statusRequests != 0 {
		t.Fatalf("status requests = %d, want 0", statusRequests)
	}
}

func shellCommandWorkspace(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	local := t.TempDir()
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-test",
		StateDir:    filepath.Join(local, ".remork", "state"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	return home, local
}

func writeShellCommandConfig(t *testing.T, home, local, serverURL string) {
	t.Helper()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		ClientID: "test-client",
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: serverURL},
		},
		Workspaces: map[string]config.Workspace{
			"ws-test": {Host: "lab", RemoteRoot: "/data/project", LocalRoot: local},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
}
