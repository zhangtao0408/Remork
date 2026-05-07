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

func TestDoctorJSONReportsReady(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	probe := fakeDaemonProbe{Roots: []string{"/data"}}
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: "http://127.0.0.1:17731"},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-test",
		StateDir:    filepath.Join(t.TempDir(), "state"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local, DaemonProbe: probe}), "doctor", "--json")
	if err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out.String())
	}
	var got struct {
		Ready    bool     `json:"ready"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode output %q: %v", out.String(), err)
	}
	if !got.Ready || len(got.Warnings) != 1 {
		t.Fatalf("doctor json = %#v, want ready with missing token warning", got)
	}
}

func TestDoctorAcceptsTokenFileWithoutMissingTokenWarning(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	tokenFile := writeTestTokenFile(t, home, "abc123\n")
	probe := fakeDaemonProbe{Roots: []string{"/data"}}
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: "http://127.0.0.1:17731", TokenFile: tokenFile},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-test",
		StateDir:    filepath.Join(t.TempDir(), "state"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local, DaemonProbe: probe}), "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	mustContain(t, out.String(), "OK: workspace is ready")
	mustNotContain(t, out.String(), "host has no token configured")
}

func TestDoctorFirstRunExplainsMissingConfig(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "doctor")
	if err == nil {
		t.Fatal("doctor without config should fail")
	}
	got := out.String()
	mustContain(t, got, "remork has not been configured")
	mustContain(t, got, "remork host add")
	mustContain(t, got, "remork init")
	mustNotContain(t, got, "config file is not readable")
}

func TestDoctorNetworkFailureIsActionableAndNotDuplicated(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: "http://127.0.0.1:1"},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws_test",
		StateDir:    filepath.Join(t.TempDir(), "state"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "doctor")
	if err == nil {
		t.Fatal("doctor returned nil error")
	}
	got := out.String()
	mustContain(t, got, "connection refused")
	mustContain(t, got, "Fix: start remorkd")
	if strings.Contains(got, "Get ") || strings.Contains(got, "/status") {
		t.Fatalf("doctor leaked raw HTTP error:\n%s", got)
	}
	if count := strings.Count(got, "connection refused"); count != 1 {
		t.Fatalf("doctor printed connection failure %d times, want once:\n%s", count, got)
	}
}

func TestDoctorAcceptsWorkspaceUnderAdvertisedParentRoot(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_ = json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/home/me"}})
		case "/manifest":
			if got := r.URL.Query().Get("root"); got != "/home/me/project" {
				t.Errorf("manifest root = %q, want /home/me/project", got)
			}
			_ = json.NewEncoder(w).Encode(api.ManifestResponse{Root: "/home/me/project", Path: "."})
		case "/operations":
			if got := r.URL.Query().Get("root"); got != "/home/me/project" {
				t.Errorf("operations root = %q, want /home/me/project", got)
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
		RemoteRoot:  "/home/me/project",
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
		Roots:          []string{"/home/me"},
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
		RemoteRoot:  "/home/me/project",
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
	if got, want := manifestRoots, []string{"/home/me/project"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("manifest probes = %v, want %v", got, want)
	}
	if got, want := operationRoots, []string{"/home/me/project"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("operation probes = %v, want %v", got, want)
	}
}
