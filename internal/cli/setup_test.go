package cli

import (
	"bytes"
	"strings"
	"testing"

	"remork/internal/output"
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
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	got := strings.Join(keys, ",")
	for _, want := range []string{"host", "ssh", "roots", "url", "addr", "token_env", "verify"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fields missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "allow_unauthenticated_network_bind") || strings.Contains(got, "dry_run") || strings.Contains(got, "yes") {
		t.Fatalf("default prepare fields should not expose advanced flags: %s", got)
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
