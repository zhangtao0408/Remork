package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/workspace"
)

func TestDebugAPIRowsKeepProbeNameAsFirstField(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/remote/root"}, Version: "test"})
		case "/manifest":
			_ = json.NewEncoder(w).Encode(api.ManifestResponse{Root: "/remote/root", Path: ".", Revision: "rev", Entries: []api.FileEntry{}})
		case "/operations":
			_ = json.NewEncoder(w).Encode(struct {
				Entries []any `json:"entries"`
			}{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: server.URL},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/remote/root",
		WorkspaceID: "ws-test",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-test"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "--color=never", "debug", "api")
	if err != nil {
		t.Fatalf("debug api: %v\nstderr=%s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("debug api lines = %d, output:\n%s", len(lines), stdout.String())
	}
	for i, wantPrefix := range []string{"status OK ", "manifest OK ", "operations OK "} {
		if !strings.HasPrefix(lines[i], wantPrefix) {
			t.Fatalf("line %d = %q, want prefix %q", i, lines[i], wantPrefix)
		}
	}
	if stderr.String() != "" {
		t.Fatalf("debug api should not write stderr on success, got %q", stderr.String())
	}
}
