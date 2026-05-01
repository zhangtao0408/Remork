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

func TestRemorkProductDoctorReportsReadyWithNoTokenWarning(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/data/project-a"}})
		case "/manifest":
			_ = json.NewEncoder(w).Encode(api.ManifestResponse{Root: "/data/project-a", Path: "."})
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
			"lab-a": {Name: "lab-a", URL: server.URL},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab-a",
		RemoteRoot:  "/data/project-a",
		WorkspaceID: "ws_test",
		StateDir:    filepath.Join(t.TempDir(), "state"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err := executeCommand(cmd, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}

	got := out.String()
	if !strings.Contains(got, "OK: workspace is ready") {
		t.Fatalf("doctor should report ready, got:\n%s", got)
	}
	if !strings.Contains(got, "WARN: host has no token configured") {
		t.Fatalf("doctor should warn about missing token, got:\n%s", got)
	}
	if !strings.Contains(got, "trusted VPN/private networks") {
		t.Fatalf("doctor warning should mention trusted VPN/private networks, got:\n%s", got)
	}
}
