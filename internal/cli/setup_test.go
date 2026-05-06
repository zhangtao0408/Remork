package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/config"
	"remork/internal/output"
	"remork/internal/workspace"
)

func TestSetupScopeDoesNotAssumeCurrentProject(t *testing.T) {
	items := setupScopeItems(false)
	if len(items) == 0 || items[0].Name != "Connect this project" {
		t.Fatalf("first setup scope = %#v", items)
	}
	foundPrepare := false
	for _, item := range items {
		if item.Name == "Only prepare a server" {
			foundPrepare = true
		}
	}
	if !foundPrepare {
		t.Fatalf("setup scopes should include server-only option: %#v", items)
	}
}

func TestSetupScopeItemsForBoundWorkspacePreferUpdate(t *testing.T) {
	items := setupScopeItems(true)
	if len(items) == 0 || !strings.Contains(items[0].Name, "Update") {
		t.Fatalf("bound setup should prefer update/repair, got %#v", items)
	}
}

func TestSetupPrepareServerBuildsDaemonAndHostSpecs(t *testing.T) {
	values := map[string]string{
		"host":       "lab",
		"ssh":        "lab.example",
		"roots":      "/data",
		"url":        "http://lab.example:17731",
		"addr":       "127.0.0.1:17731",
		"local_bin":  "/tmp/remorkd",
		"remote_bin": ".local/bin/remorkd",
		"token_env":  "REMORK_TOKEN",
		"no_proxy":   "yes",
		"verify":     "yes",
	}
	spec, host, err := setupPrepareServerSpecs(values)
	if err != nil {
		t.Fatalf("setupPrepareServerSpecs: %v", err)
	}
	if spec.Action != "install" || spec.HostName != "lab" || spec.SSHTarget != "lab.example" {
		t.Fatalf("daemon spec = %#v", spec)
	}
	if host.Name != "lab" || host.URL != "http://lab.example:17731" || !host.NoProxy {
		t.Fatalf("host spec = %#v", host)
	}
}

func TestSetupPrepareServerFieldsAreMinimal(t *testing.T) {
	fields := setupPrepareServerFields(nil)
	keys := make([]string, 0, len(fields))
	tokenPlaceholder := ""
	for _, field := range fields {
		keys = append(keys, field.Key)
		if field.Key == "token_env" {
			tokenPlaceholder = field.Placeholder
		}
	}
	got := strings.Join(keys, ",")
	for _, want := range []string{"host", "ssh", "roots", "port", "token_env", "verify"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fields missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "local_bin") || strings.Contains(got, "remote_bin") || strings.Contains(got, "url") || strings.Contains(got, "addr") || strings.Contains(got, "allow_unauthenticated_network_bind") || strings.Contains(got, "dry_run") || strings.Contains(got, "yes") {
		t.Fatalf("default prepare fields should not expose advanced flags: %s", got)
	}
	if tokenPlaceholder != "" {
		t.Fatalf("token env placeholder should be empty so it is not mistaken for a configured value, got %q", tokenPlaceholder)
	}
}

func TestSetupPrepareServerSpecDerivesURLAddrAndTokenFile(t *testing.T) {
	values := map[string]string{
		"host":      "lab",
		"ssh":       "lab.example",
		"roots":     "/data",
		"port":      "17731",
		"token_env": "REMORK_TOKEN",
		"verify":    "yes",
	}
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		t.Fatalf("setupPrepareServerSpecs: %v", err)
	}
	if spec.URL != "http://lab.example:17731" || spec.Addr != "0.0.0.0:17731" {
		t.Fatalf("derived network values url=%q addr=%q", spec.URL, spec.Addr)
	}
	if spec.TokenFile != ".remork/remork.token" {
		t.Fatalf("token file = %q, want default", spec.TokenFile)
	}
	if spec.LocalBin != "" || spec.RemoteBin != "" {
		t.Fatalf("binary paths should be hidden defaults, got local=%q remote=%q", spec.LocalBin, spec.RemoteBin)
	}
}

func TestSetupCurrentServerInitialValuesFromBoundWorkspace(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab": {Name: "lab", URL: "http://lab.example:17731", TokenEnv: "REMORK_TOKEN", NoProxy: true},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-setup",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-setup"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	values := setupCurrentServerInitialValues(Options{
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: fakeDaemonProbe{Roots: []string{"/data"}},
	})

	for key, want := range map[string]string{
		"host":      "lab",
		"ssh":       "lab.example",
		"roots":     "/data",
		"port":      "17731",
		"token_env": "REMORK_TOKEN",
		"no_proxy":  "yes",
		"verify":    "yes",
	} {
		if got := values[key]; got != want {
			t.Fatalf("initial[%s] = %q, want %q; all=%#v", key, got, want, values)
		}
	}
}

func TestSetupCurrentServerInitialValuesPreferSharedURLSSHHost(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"z00879328_docker":     {Name: "z00879328_docker", URL: "http://175.100.2.7:17731", NoProxy: true},
		"z00879328_docker_2.7": {Name: "z00879328_docker_2.7", URL: "http://175.100.2.7:17731", NoProxy: true},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "z00879328_docker_2.7",
		RemoteRoot:  "/home/z00879328/project",
		WorkspaceID: "ws-setup",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-setup"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	values := setupCurrentServerInitialValues(Options{HomeDir: home, WorkingDir: local})

	if values["host"] != "z00879328_docker_2.7" {
		t.Fatalf("host = %q", values["host"])
	}
	if values["ssh"] != "z00879328_docker" {
		t.Fatalf("ssh = %q, want shared URL alias z00879328_docker; all=%#v", values["ssh"], values)
	}
}

func TestSetupUpdatePreservesUnauthenticatedPrivateNetworkHost(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab": {Name: "lab", URL: "http://10.0.0.5:17731", NoProxy: true},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-setup",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-setup"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	values := setupCurrentServerInitialValues(Options{HomeDir: home, WorkingDir: local})
	spec, err := setupUpdateServerSpec(values)
	if err != nil {
		t.Fatalf("setupUpdateServerSpec: %v", err)
	}

	if spec.TokenEnv != "" || spec.TokenFile != "" {
		t.Fatalf("unauthenticated host should not invent token auth: %#v", spec)
	}
	if !spec.AllowUnauthenticatedNetworkBind {
		t.Fatalf("update should preserve existing no-token private-network host: %#v values=%#v", spec, values)
	}
}

func TestSetupDaemonDeployOptionsCarryUnauthenticatedBind(t *testing.T) {
	spec := DaemonDeploySpec{
		Action:                          "upgrade",
		HostName:                        "lab",
		SSHTarget:                       "lab-ssh",
		Roots:                           []string{"/data"},
		Addr:                            "0.0.0.0:17731",
		URL:                             "http://lab:17731",
		LocalBin:                        fakeDaemonBinary(t),
		NoProxy:                         true,
		Verify:                          true,
		AllowUnauthenticatedNetworkBind: true,
	}

	deploy := setupDaemonDeployOptionsFromSpec(spec, Options{Version: "test"}, output.ColorNever)

	if !deploy.allowUnauthenticatedNetworkBind {
		t.Fatalf("deploy should carry unauthenticated bind approval: %#v", deploy)
	}
	if err := validateDaemonDeployExecution(deploy); err != nil {
		t.Fatalf("execution validation should allow trusted private-network bind: %v", err)
	}
}

func TestSetupConnectProjectBuildsWorkspaceBindSpec(t *testing.T) {
	values := map[string]string{
		"host":        "lab",
		"remote_root": "/data/project",
		"first_sync":  "yes",
	}
	bind, firstSync, err := setupConnectProjectSpec("/local/project", values)
	if err != nil {
		t.Fatalf("setupConnectProjectSpec: %v", err)
	}
	if bind.HostName != "lab" || bind.RemoteRoot != "/data/project" || bind.LocalRoot != "/local/project" {
		t.Fatalf("bind spec = %#v", bind)
	}
	if !firstSync {
		t.Fatal("firstSync should be true")
	}
}

func TestSetupUpdateServerUsesUpgradeAction(t *testing.T) {
	values := map[string]string{
		"host":       "lab",
		"ssh":        "lab.example",
		"roots":      "/data",
		"url":        "http://lab.example:17731",
		"addr":       "127.0.0.1:17731",
		"local_bin":  "/tmp/remorkd",
		"remote_bin": ".local/bin/remorkd",
		"verify":     "yes",
	}
	spec, err := setupUpdateServerSpec(values)
	if err != nil {
		t.Fatalf("setupUpdateServerSpec: %v", err)
	}
	if spec.Action != "upgrade" || !spec.Verify {
		t.Fatalf("update spec = %#v", spec)
	}
}

func TestRenderSetupPlanIncludesActionsAndNext(t *testing.T) {
	var buf bytes.Buffer
	plan := OperationPlan{
		Title:  "Setup plan",
		Target: map[string]string{"host": "lab"},
		Actions: []PlannedAction{
			{Label: "prepare remote directories"},
			{Label: "copy remorkd binary"},
		},
		Next: []string{"remork init lab:/data/project"},
	}
	renderSetupPlan(&buf, output.ColorNever, plan)
	got := buf.String()
	for _, want := range []string{"Setup plan", "host", "prepare remote directories", "copy remorkd binary", "remork init"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered setup plan missing %q:\n%s", want, got)
		}
	}
}

func TestSetupNonInteractiveReturnsAdvancedCommandGuidance(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir()})
	_, err := executeCommand(cmd, "setup", "--non-interactive")
	if err == nil {
		t.Fatal("setup --non-interactive should fail")
	}
	for _, want := range []string{"interactive terminal", "remork host add", "remork init"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("setup error should contain %q, got %v", want, err)
		}
	}
}

func TestSetupPrepareServerExecutionRendersPlanBeforeExecute(t *testing.T) {
	localBin := fakeDaemonBinary(t)
	values := map[string]string{
		"host":       "lab",
		"ssh":        "lab.example",
		"roots":      "/data",
		"url":        "http://lab.example:17731",
		"addr":       "127.0.0.1:17731",
		"local_bin":  localBin,
		"remote_bin": ".local/bin/remorkd",
		"no_proxy":   "yes",
		"verify":     "no",
	}
	var out bytes.Buffer
	err := executeSetupPrepareServerPlan(&out, output.ColorNever, values)
	if err != nil {
		t.Fatalf("executeSetupPrepareServerPlan: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Daemon install", "lab", "prepare remote directories", "copy remorkd binary"} {
		if !strings.Contains(got, want) {
			t.Fatalf("setup prepare plan missing %q:\n%s", want, got)
		}
	}
}
