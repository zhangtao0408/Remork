package cli

import (
	"context"
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

type fakeDaemonProbe struct {
	Roots []string
}

func (p fakeDaemonProbe) Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error) {
	return api.StatusResponse{Roots: p.Roots}, nil
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test-version"})
	out, err := executeCommand(cmd, "version")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "remork test-version" {
		t.Fatalf("output %q", out.String())
	}
}

func TestRootHelpShowsProductCommandLayers(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "help")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	mustContain(t, out.String(), "Must know: init sync status apply run shell")
	mustContain(t, out.String(), "Learn later: pull diff restore log watch")
	mustContain(t, out.String(), "Debug and operations: doctor debug daemon")
}

func TestRunIsVisibleAndExecIsAlias(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "help")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	mustContain(t, out.String(), "run")
	mustNotContain(t, out.String(), "exec")

	execCmd, _, err := cmd.Find([]string{"exec"})
	if err != nil {
		t.Fatalf("find exec: %v", err)
	}
	if execCmd == nil || execCmd.Name() != "exec" {
		t.Fatalf("exec command not found: %#v", execCmd)
	}
	if !execCmd.Hidden {
		t.Fatalf("exec command should be hidden")
	}
}

func TestExecAliasUsesRunPlaceholderHandler(t *testing.T) {
	runCmd := NewRootCommand(Options{Version: "test"})
	_, runErr := executeCommand(runCmd, "run")
	if runErr == nil {
		t.Fatal("run should return the product placeholder error")
	}

	execCmd := NewRootCommand(Options{Version: "test"})
	_, execErr := executeCommand(execCmd, "exec")
	if execErr == nil {
		t.Fatal("exec should return the run placeholder error")
	}

	if execErr.Error() != runErr.Error() {
		t.Fatalf("exec error %q, want run error %q", execErr.Error(), runErr.Error())
	}
}

func TestRootCommandSilencesCobraErrorPrinting(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "run")
	if err == nil {
		t.Fatal("run should return the product placeholder error")
	}

	if out.String() != "" {
		t.Fatalf("expected cobra to leave error output empty, got %q", out.String())
	}
}

func TestHostAddWritesConfig(t *testing.T) {
	home := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	_, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://10.0.0.12:17731", "--token-env", "REMORK_TOKEN", "--no-proxy")
	if err != nil {
		t.Fatalf("host add: %v", err)
	}

	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	host := cfg.Hosts["lab-a"]
	if host.Name != "lab-a" || host.URL != "http://10.0.0.12:17731" || host.TokenEnv != "REMORK_TOKEN" || !host.NoProxy {
		t.Fatalf("bad host config: %#v", host)
	}
}

func TestConfigStoreRequiresHomeDir(t *testing.T) {
	_, err := configStore(Options{})
	if err == nil {
		t.Fatal("configStore should fail when home dir is unavailable")
	}
	mustContain(t, err.Error(), "home directory")
}

func TestInitWritesLocalBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: fakeDaemonProbe{Roots: []string{"/data/project-a"}},
	})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://10.0.0.12:17731"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err != nil {
		t.Fatalf("init: %v", err)
	}

	binding, root, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if root != local {
		t.Fatalf("root %q, want %q", root, local)
	}
	if binding.Host != "lab-a" || binding.RemoteRoot != "/data/project-a" {
		t.Fatalf("bad binding: %#v", binding)
	}
	if !filepath.IsAbs(binding.StateDir) {
		t.Fatalf("state dir should be absolute: %q", binding.StateDir)
	}
	if binding.Token != "" {
		t.Fatal("binding should not contain token")
	}
}

func TestInitUsesDefaultDaemonProbe(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	var statusRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		statusRequests++
		if r.Header.Get(api.HeaderClientID) == "" {
			t.Errorf("missing %s header", api.HeaderClientID)
		}
		if err := json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/data/project-a"}}); err != nil {
			t.Errorf("encode status: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", server.URL); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err != nil {
		t.Fatalf("init: %v", err)
	}

	if statusRequests != 1 {
		t.Fatalf("status requests = %d, want 1", statusRequests)
	}
	if _, _, err := workspace.ResolveFrom(local); err != nil {
		t.Fatalf("binding should be written after advertised root check: %v", err)
	}
}

func TestInitDefaultProbeRejectsUnadvertisedRoot(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	var statusRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		statusRequests++
		if err := json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/data/other"}}); err != nil {
			t.Errorf("encode status: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", server.URL); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err == nil {
		t.Fatal("init should reject a root that is not advertised by the daemon")
	}

	if statusRequests != 1 {
		t.Fatalf("status requests = %d, want 1", statusRequests)
	}
	if _, _, err := workspace.ResolveFrom(local); err == nil {
		t.Fatal("binding should not be written when the daemon does not advertise the root")
	}
}
