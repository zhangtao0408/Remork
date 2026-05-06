package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/config"
	"remork/internal/workspace"
)

func TestWorkspaceCommandShowsCurrentBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	stateDir := filepath.Join(home, ".remork", "state", "ws_test")
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws_test",
		StateDir:    stateDir,
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err := executeCommand(cmd, "workspace")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	mustContain(t, out.String(), "== Workspace ==")
	mustContain(t, out.String(), "local:")
	mustContain(t, out.String(), local)
	mustContain(t, out.String(), "host: lab")
	mustContain(t, out.String(), "workspace root: /data/project")
	mustContain(t, out.String(), "workspace_id: ws_test")
	mustContain(t, out.String(), "state_scope: local-checkout")
	mustNotContain(t, out.String(), "token")
}

func TestWorkspaceCommandPrintsJSONBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	stateDir := filepath.Join(home, ".remork", "state", "ws_test")
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws_test",
		StateDir:    stateDir,
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "workspace", "--json")
	if err != nil {
		t.Fatalf("workspace --json: %v", err)
	}
	var got struct {
		LocalRoot   string `json:"local_root"`
		Host        string `json:"host"`
		RemoteRoot  string `json:"remote_root"`
		WorkspaceID string `json:"workspace_id"`
		StateDir    string `json:"state_dir"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode workspace json %q: %v", out.String(), err)
	}
	if got.LocalRoot != local || got.Host != "lab" || got.RemoteRoot != "/data/project" || got.WorkspaceID != "ws_test" || got.StateDir != stateDir {
		t.Fatalf("workspace json = %#v", got)
	}
}

func TestWorkspaceRemoveDeletesLocalBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws_test",
		StateDir:    filepath.Join(home, ".remork", "state", "ws_test"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err := executeCommand(cmd, "workspace", "remove")
	if err != nil {
		t.Fatalf("workspace remove: %v", err)
	}
	mustContain(t, out.String(), "removed workspace binding")
	if _, err := os.Stat(filepath.Join(local, workspace.MarkerName)); !os.IsNotExist(err) {
		t.Fatalf("binding marker still exists: %v", err)
	}
	if _, _, err := workspace.ResolveFrom(local); err == nil {
		t.Fatal("binding should no longer resolve")
	}
}

func TestWorkspaceListShowsRegisteredWorkspaces(t *testing.T) {
	home := t.TempDir()
	localA := t.TempDir()
	localB := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{},
		Workspaces: map[string]config.Workspace{
			"ws-a": {Host: "lab-a", RemoteRoot: "/data/a", LocalRoot: localA},
			"ws-b": {Host: "lab-b", RemoteRoot: "/data/b", LocalRoot: localB},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err := executeCommand(cmd, "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list: %v", err)
	}
	mustContain(t, out.String(), "== Workspaces ==")
	for _, want := range []string{"ws-a", "lab-a", "/data/a", localA, "ws-b", "lab-b", "/data/b", localB} {
		mustContain(t, out.String(), want)
	}
}

func TestWorkspaceListPrintsJSON(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{},
		Workspaces: map[string]config.Workspace{
			"ws-a": {Host: "lab-a", RemoteRoot: "/data/a", LocalRoot: local},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home}), "workspace", "list", "--json")
	if err != nil {
		t.Fatalf("workspace list --json: %v", err)
	}
	var got struct {
		Workspaces map[string]config.Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode workspace list json %q: %v", out.String(), err)
	}
	if got.Workspaces["ws-a"].LocalRoot != local {
		t.Fatalf("workspace list json = %#v", got)
	}
}

func TestWorkspaceHelpShowsSubcommands(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "workspace", "--help")
	if err != nil {
		t.Fatalf("workspace help: %v", err)
	}
	mustContain(t, out.String(), "remove")
	mustContain(t, out.String(), "list")
	mustNotContain(t, out.String(), "Must know: init sync")
}

func TestHostListAndRemove(t *testing.T) {
	home := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab-a": {Name: "lab-a", URL: "http://127.0.0.1:18101", NoProxy: true},
		"lab-b": {Name: "lab-b", URL: "http://127.0.0.1:18102", TokenEnv: "REMORK_TOKEN"},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err := executeCommand(cmd, "host", "list")
	if err != nil {
		t.Fatalf("host list: %v", err)
	}
	mustContain(t, out.String(), "== Hosts ==")
	mustContain(t, out.String(), "lab-a")
	mustContain(t, out.String(), "no_proxy")
	mustContain(t, out.String(), "lab-b")
	mustContain(t, out.String(), "REMORK_TOKEN")

	cmd = NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err = executeCommand(cmd, "host", "remove", "lab-a")
	if err != nil {
		t.Fatalf("host remove: %v", err)
	}
	mustContain(t, out.String(), "== Host removed ==")
	mustContain(t, out.String(), "removed host lab-a")
	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := cfg.Hosts["lab-a"]; ok {
		t.Fatal("lab-a should be removed")
	}
	if _, ok := cfg.Hosts["lab-b"]; !ok {
		t.Fatal("lab-b should remain")
	}
}

func TestHostAddAndListPrintJSON(t *testing.T) {
	home := t.TempDir()
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home}), "host", "add", "lab", "--url", "http://127.0.0.1:17731", "--no-proxy", "--json")
	if err != nil {
		t.Fatalf("host add --json: %v", err)
	}
	var added config.Host
	if err := json.Unmarshal(out.Bytes(), &added); err != nil {
		t.Fatalf("decode host add json %q: %v", out.String(), err)
	}
	if added.Name != "lab" || added.URL != "http://127.0.0.1:17731" || !added.NoProxy {
		t.Fatalf("host add json = %#v", added)
	}

	out, err = executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home}), "host", "list", "--json")
	if err != nil {
		t.Fatalf("host list --json: %v", err)
	}
	var got struct {
		Hosts map[string]config.Host `json:"hosts"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode host list json %q: %v", out.String(), err)
	}
	if got.Hosts["lab"].URL != "http://127.0.0.1:17731" {
		t.Fatalf("host list json = %#v", got)
	}
}

func TestHostAddPrintsConfirmationAndRejectsInvalidURL(t *testing.T) {
	home := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err := executeCommand(cmd, "host", "add", "lab", "--url", "http://127.0.0.1:17731", "--no-proxy")
	if err != nil {
		t.Fatalf("host add: %v", err)
	}
	mustContain(t, out.String(), "saved host lab")
	mustContain(t, out.String(), "remork daemon status lab")

	cmd = NewRootCommand(Options{Version: "test", HomeDir: home})
	_, err = executeCommand(cmd, "host", "add", "bad", "--url", "daemon-host")
	if err == nil {
		t.Fatal("host add should reject URL without scheme")
	}
	mustContain(t, err.Error(), "daemon URL must include http:// or https://")
}

func TestHostRemoveMissingFails(t *testing.T) {
	home := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	if _, err := executeCommand(cmd, "host", "remove", "missing"); err == nil {
		t.Fatal("host remove should fail for missing host")
	}
}
