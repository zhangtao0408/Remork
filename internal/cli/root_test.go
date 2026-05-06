package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/limits"
	"remork/internal/ops"
	"remork/internal/workspace"
)

type fakeDaemonProbe struct {
	Roots          []string
	ManifestRoots  *[]string
	OperationRoots *[]string
}

func (p fakeDaemonProbe) Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error) {
	return api.StatusResponse{Roots: p.Roots}, nil
}

func (p fakeDaemonProbe) Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error) {
	if p.ManifestRoots != nil {
		*p.ManifestRoots = append(*p.ManifestRoots, root)
	}
	return api.ManifestResponse{Root: root, Path: "."}, nil
}

func (p fakeDaemonProbe) Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error) {
	if p.OperationRoots != nil {
		*p.OperationRoots = append(*p.OperationRoots, root)
	}
	return nil, nil
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

	mustContain(t, out.String(), "Setup:")
	mustContain(t, out.String(), "Daily:")
	mustContain(t, out.String(), "Observe:")
	mustContain(t, out.String(), "Diagnose:")
	mustContain(t, out.String(), "Advanced:")
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

func TestDebugCommandsAreRegistered(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	for _, args := range [][]string{{"debug", "manifest"}, {"debug", "events"}, {"debug", "api"}} {
		found, _, err := cmd.Find(args)
		if err != nil || found == nil {
			t.Fatalf("command %v not registered: %v", args, err)
		}
		if found.Name() != args[len(args)-1] {
			t.Fatalf("command %v resolved to %q, want %q", args, found.Name(), args[len(args)-1])
		}
	}
}

func TestDaemonCommandsAreRegistered(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	for _, args := range [][]string{{"daemon", "install"}, {"daemon", "upgrade"}, {"daemon", "status"}} {
		found, _, err := cmd.Find(args)
		if err != nil || found == nil {
			t.Fatalf("command %v not registered: %v", args, err)
		}
		if found.Name() != args[len(args)-1] {
			t.Fatalf("command %v resolved to %q, want %q", args, found.Name(), args[len(args)-1])
		}
	}
}

func TestSubcommandHelpShowsCommandFlags(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "daemon", "install", "--help")
	if err != nil {
		t.Fatalf("execute help: %v", err)
	}
	for _, want := range []string{"Flags:", "--root", "--ssh", "--url", "--platform", "--addr", "--dry-run", "--execute", "--verify"} {
		mustContain(t, out.String(), want)
	}
}

func TestInitHelpIsSelfDescribing(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "init", "--help")
	if err != nil {
		t.Fatalf("execute help: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"When to use:",
		"Interactive:",
		"Automation:",
		"remork init HOST:/absolute/remote/path",
		"remork sync",
	} {
		mustContain(t, got, want)
	}
}

func TestVisibleCommandsHaveDetailedHelp(t *testing.T) {
	root := NewRootCommand(Options{Version: "test"})
	var check func(*cobra.Command)
	check = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			if child.Hidden || child.Name() == "help" || child.Name() == "version" {
				continue
			}
			if child.Long == "" || child.Long == child.Short {
				t.Fatalf("%s help needs a detailed Long description", child.CommandPath())
			}
			if child.Example == "" {
				t.Fatalf("%s help needs runnable examples", child.CommandPath())
			}
			check(child)
		}
	}
	check(root)
}

func TestRootMenuRequiredInputItemsOpenHelp(t *testing.T) {
	items := rootCommandItems(true)
	required := map[string]bool{
		"run":            true,
		"pull":           true,
		"daemon status":  true,
		"daemon install": true,
		"daemon upgrade": true,
		"init":           true,
	}
	for _, item := range items {
		if !required[item.Name] {
			continue
		}
		if !item.HelpOnly {
			t.Fatalf("root menu item %q should open help instead of executing without required input", item.Name)
		}
		args := item.Args
		if len(args) == 0 || args[len(args)-1] == "--help" {
			t.Fatalf("root menu item %q should store base args and let menu append --help, args=%v", item.Name, args)
		}
	}
	for name := range required {
		found := false
		for _, item := range items {
			found = found || item.Name == name
		}
		if !found {
			t.Fatalf("required input root menu item %q not found", name)
		}
	}
}

func TestRootMenuIncludesDaemonInstallAndUpgrade(t *testing.T) {
	items := rootCommandItems(false)
	want := map[string]bool{
		"daemon install": false,
		"daemon upgrade": false,
	}
	for _, item := range items {
		if _, ok := want[item.Name]; ok {
			want[item.Name] = true
			if item.Group != "Advanced" {
				t.Fatalf("daemon operation %q should be in Advanced group, got %#v", item.Name, item)
			}
			if len(item.Args) < 2 || item.Args[0] != "daemon" {
				t.Fatalf("daemon operation %q should launch daemon subcommand, args=%v", item.Name, item.Args)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("root menu should include %q", name)
		}
	}
}

func TestRootMenuPrioritizesSetupWhenDirectoryIsUnbound(t *testing.T) {
	items := rootCommandItems(false)
	if len(items) == 0 {
		t.Fatal("root menu should contain commands")
	}
	if items[0].Group != "Setup" || items[0].Name != "setup" {
		t.Fatalf("first unbound menu item = %#v, want setup", items[0])
	}
}

func TestRootMenuKeepsDailyCommandsFirstWhenDirectoryIsBound(t *testing.T) {
	items := rootCommandItems(true)
	if len(items) == 0 {
		t.Fatal("root menu should contain commands")
	}
	if items[0].Group != "Daily" || items[0].Name != "sync" {
		t.Fatalf("first bound menu item = %#v, want daily sync", items[0])
	}
}

func TestDaemonInstallAcceptsRepeatedRootFlags(t *testing.T) {
	home := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err := executeCommand(cmd, "daemon", "install", "lab", "--root", "/data", "--root", "/scratch", "--local-bin", fakeDaemonBinary(t), "--dry-run")
	if err != nil {
		t.Fatalf("daemon install: %v", err)
	}
	mustContain(t, out.String(), "/data")
	mustContain(t, out.String(), "/scratch")
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
	_, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://remork-daemon.example.internal:17731", "--token-env", "REMORK_TOKEN", "--no-proxy")
	if err != nil {
		t.Fatalf("host add: %v", err)
	}

	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	host := cfg.Hosts["lab-a"]
	if host.Name != "lab-a" || host.URL != "http://remork-daemon.example.internal:17731" || host.TokenEnv != "REMORK_TOKEN" || !host.NoProxy {
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
	var manifestRoots []string
	probe := fakeDaemonProbe{Roots: []string{"/data/project-a"}, ManifestRoots: &manifestRoots}
	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: probe,
	})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://remork-daemon.example.internal:17731"); err != nil {
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
	if got, want := manifestRoots, []string{"/data/project-a"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("manifest probes = %v, want %v", got, want)
	}
}

func TestInitWithoutConfigReturnsHelpfulFirstRunError(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "init", "lab:/data/project")
	if err == nil {
		t.Fatal("init without config should fail")
	}
	if strings.Contains(err.Error(), "config.json") || strings.Contains(out.String(), "config.json") {
		t.Fatalf("init leaked raw config path: err=%v out=%q", err, out.String())
	}
	if !strings.Contains(err.Error(), "remork is not configured") || !strings.Contains(err.Error(), "remork host add lab --url URL") {
		t.Fatalf("init error = %v, want first-run host add guidance", err)
	}
}

func TestInitAcceptsWorkspaceUnderAdvertisedParentRoot(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	var manifestRoots []string
	probe := fakeDaemonProbe{Roots: []string{"/home/me"}, ManifestRoots: &manifestRoots}
	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: probe,
	})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://remork-daemon.example.internal:17731"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/home/me/project"); err != nil {
		t.Fatalf("init: %v", err)
	}

	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if binding.RemoteRoot != "/home/me/project" {
		t.Fatalf("remote root = %q", binding.RemoteRoot)
	}
	if got, want := manifestRoots, []string{"/home/me/project"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("manifest probes = %v, want %v", got, want)
	}
}

func TestInitRejectsWorkspaceSiblingOfAdvertisedRoot(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	var manifestRoots []string
	probe := fakeDaemonProbe{Roots: []string{"/home/me"}, ManifestRoots: &manifestRoots}
	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: probe,
	})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://remork-daemon.example.internal:17731"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/home/me_sibling"); err == nil {
		t.Fatal("init should reject sibling prefix of advertised root")
	} else {
		mustContain(t, err.Error(), "outside advertised allowed roots")
	}
	if len(manifestRoots) != 0 {
		t.Fatalf("manifest should not be probed for outside workspace, got %v", manifestRoots)
	}
	if _, _, err := workspace.ResolveFrom(local); err == nil {
		t.Fatal("binding should not be written when the workspace is outside advertised roots")
	}
}

func TestInitUsesDifferentStateDirForDifferentLocalRoots(t *testing.T) {
	home := t.TempDir()
	localA := t.TempDir()
	localB := t.TempDir()

	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  localA,
		DaemonProbe: fakeDaemonProbe{Roots: []string{"/data/project-a"}},
	})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://remork-daemon.example.internal:17731"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err != nil {
		t.Fatalf("init A: %v", err)
	}

	cmd = NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  localB,
		DaemonProbe: fakeDaemonProbe{Roots: []string{"/data/project-a"}},
	})
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err != nil {
		t.Fatalf("init B: %v", err)
	}

	bindingA, _, err := workspace.ResolveFrom(localA)
	if err != nil {
		t.Fatalf("resolve binding A: %v", err)
	}
	bindingB, _, err := workspace.ResolveFrom(localB)
	if err != nil {
		t.Fatalf("resolve binding B: %v", err)
	}
	if bindingA.StateDir == bindingB.StateDir {
		t.Fatalf("state dirs shared: %s", bindingA.StateDir)
	}
}

func TestStableWorkspaceIDUsesDelimiterSafePartBoundaries(t *testing.T) {
	first := stableWorkspaceID("a\x00b", "c")
	second := stableWorkspaceID("a", "b\x00c")

	if first == second {
		t.Fatalf("workspace IDs collide: %s", first)
	}
}

func TestInitUsesDefaultDaemonProbe(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	var statusRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			statusRequests++
			if r.Header.Get(api.HeaderClientID) == "" {
				t.Errorf("missing %s header", api.HeaderClientID)
			}
			if err := json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/data/project-a"}}); err != nil {
				t.Errorf("encode status: %v", err)
			}
		case "/manifest":
			if got := r.URL.Query().Get("root"); got != "/data/project-a" {
				t.Errorf("manifest root = %q, want /data/project-a", got)
			}
			_ = json.NewEncoder(w).Encode(api.ManifestResponse{Root: "/data/project-a", Path: "."})
		default:
			http.NotFound(w, r)
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

func TestInitDefaultProbeSendsTokenFromHostEnv(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	t.Setenv("REMORK_TOKEN", "abc123")
	var statusRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			statusRequests++
			if got := r.Header.Get(api.HeaderClientID); got == "" {
				t.Errorf("missing %s header", api.HeaderClientID)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer abc123" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			if err := json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/data/project-a"}}); err != nil {
				t.Errorf("encode status: %v", err)
			}
		case "/manifest":
			if got := r.Header.Get("Authorization"); got != "Bearer abc123" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(api.ManifestResponse{Root: "/data/project-a", Path: "."})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", server.URL, "--token-env", "REMORK_TOKEN"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err != nil {
		t.Fatalf("init: %v", err)
	}

	if statusRequests != 1 {
		t.Fatalf("status requests = %d, want 1", statusRequests)
	}
	if _, _, err := workspace.ResolveFrom(local); err != nil {
		t.Fatalf("binding should be written after authenticated status check: %v", err)
	}
}

func TestHTTPDaemonProbeStatusBoundsErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", limits.MaxErrorBodyBytes) + "SHOULD_NOT_APPEAR"))
	}))
	t.Cleanup(server.Close)

	_, err := (httpDaemonProbe{}).Status(context.Background(), config.Host{URL: server.URL}, "test-client")
	if err == nil {
		t.Fatal("expected daemon status error")
	}
	if strings.Contains(err.Error(), "SHOULD_NOT_APPEAR") {
		t.Fatalf("error body was not bounded: %q", err.Error())
	}
}

func TestInitDefaultProbeMissingTokenEnvDoesNotWriteBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("status should not be requested when token env is missing")
	}))
	t.Cleanup(server.Close)

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", server.URL, "--token-env", "REMORK_TOKEN_MISSING"); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err == nil {
		t.Fatal("init should fail when token env is missing")
	} else if !strings.Contains(err.Error(), "REMORK_TOKEN_MISSING") {
		t.Fatalf("error %q should mention missing token env", err.Error())
	}

	if _, _, err := workspace.ResolveFrom(local); err == nil {
		t.Fatal("binding should not be written when token env is missing")
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
	} else {
		mustContain(t, err.Error(), "outside advertised allowed roots")
	}

	if statusRequests != 1 {
		t.Fatalf("status requests = %d, want 1", statusRequests)
	}
	if _, _, err := workspace.ResolveFrom(local); err == nil {
		t.Fatal("binding should not be written when the daemon does not advertise the root")
	}
}

func TestRootIncludesSetupCommand(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	setup, _, err := cmd.Find([]string{"setup"})
	if err != nil {
		t.Fatalf("find setup: %v", err)
	}
	if setup == nil || setup.Name() != "setup" {
		t.Fatalf("setup command = %#v", setup)
	}
}

func TestRootHelpPromotesSetupFirst(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test"}), "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	text := out.String()
	mustContain(t, text, "Setup:")
	mustContain(t, text, "setup")
	if strings.Index(text, "setup") > strings.Index(text, "daemon") {
		t.Fatalf("setup should appear before daemon in help:\n%s", text)
	}
}
