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

func TestDoctorAcceptsWorkspaceUnderAdvertisedParentRoot(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/home/z00879328"}})
		case "/manifest":
			if got := r.URL.Query().Get("root"); got != "/home/z00879328/11_Wan22_Adapt" {
				t.Errorf("manifest root = %q, want /home/z00879328/11_Wan22_Adapt", got)
			}
			_ = json.NewEncoder(w).Encode(api.ManifestResponse{Root: "/home/z00879328/11_Wan22_Adapt", Path: "."})
		case "/operations":
			if got := r.URL.Query().Get("root"); got != "/home/z00879328/11_Wan22_Adapt" {
				t.Errorf("operations root = %q, want /home/z00879328/11_Wan22_Adapt", got)
			}
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
		RemoteRoot:  "/home/z00879328/11_Wan22_Adapt",
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
	mustContain(t, out.String(), "OK: workspace is ready")
}

func TestDoctorUsesInjectedDaemonProbe(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	var manifestRoots []string
	var operationRoots []string
	probe := fakeDaemonProbe{
		Roots:          []string{"/home/z00879328"},
		ManifestRoots:  &manifestRoots,
		OperationRoots: &operationRoots,
	}

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab-a": {Name: "lab-a", URL: "http://127.0.0.1:1"},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab-a",
		RemoteRoot:  "/home/z00879328/11_Wan22_Adapt",
		WorkspaceID: "ws_test",
		StateDir:    filepath.Join(t.TempDir(), "state"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local, DaemonProbe: probe})
	out, err := executeCommand(cmd, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	mustContain(t, out.String(), "OK: workspace is ready")
	if got, want := manifestRoots, []string{"/home/z00879328/11_Wan22_Adapt"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("manifest probes = %v, want %v", got, want)
	}
	if got, want := operationRoots, []string{"/home/z00879328/11_Wan22_Adapt"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("operation probes = %v, want %v", got, want)
	}
}
