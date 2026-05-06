package cli

import (
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/config"
	"remork/internal/workspace"
)

func TestJSONCommandsPrintStructuredUnboundWorkspaceError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "status", args: []string{"status", "--json"}},
		{name: "apply", args: []string{"apply", "--json"}},
		{name: "sync", args: []string{"sync", "--json"}},
		{name: "pull", args: []string{"pull", "a.txt", "--json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir()}), tt.args...)
			if err == nil {
				t.Fatalf("%v returned nil error, want unbound workspace error", tt.args)
			}
			var got struct {
				Error string `json:"error"`
				Fix   string `json:"fix"`
				Code  int    `json:"code"`
			}
			if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil {
				t.Fatalf("output is not strict JSON: %q: %v", out.String(), jsonErr)
			}
			if !strings.Contains(got.Error, "not bound") || !strings.Contains(got.Fix, "remork init") || got.Code == 0 {
				t.Fatalf("json error = %#v", got)
			}
		})
	}
}

func TestHostAddJSONPrintsStructuredUsageError(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()}), "host", "add", "lab", "--json")
	if err == nil {
		t.Fatal("host add --json without --url returned nil error")
	}
	var got struct {
		Error string `json:"error"`
		Fix   string `json:"fix"`
		Code  int    `json:"code"`
	}
	if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil {
		t.Fatalf("output is not strict JSON: %q: %v", out.String(), jsonErr)
	}
	if got.Error != "--url is required" || !strings.Contains(got.Fix, "remork host add lab --url") || got.Code == 0 {
		t.Fatalf("json error = %#v", got)
	}
}

func TestJSONCommandsPrintStructuredDaemonNetworkErrors(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	url := closedLoopbackURL(t)
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: url, NoProxy: true},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-json-network",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-json-network"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	for _, args := range [][]string{{"status", "--json"}, {"sync", "--json"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), args...)
			if err == nil {
				t.Fatalf("%v returned nil error", args)
			}
			var got commandErrorJSON
			if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil {
				t.Fatalf("output is not strict JSON: %q: %v", out.String(), jsonErr)
			}
			if !strings.Contains(got.Error, "connection refused") || !strings.Contains(got.Fix, "remork") || got.Code == 0 {
				t.Fatalf("json error = %#v", got)
			}
		})
	}
}

func TestSyncJSONPrintsStructuredMissingTokenEnvError(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: "http://127.0.0.1:17731", TokenEnv: "REMORK_TOKEN"},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-json-token",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-json-token"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "sync", "--json")
	if err == nil {
		t.Fatal("sync --json returned nil error")
	}
	var got commandErrorJSON
	if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil {
		t.Fatalf("output is not strict JSON: %q: %v", out.String(), jsonErr)
	}
	if !strings.Contains(got.Error, "REMORK_TOKEN") || !strings.Contains(got.Fix, "export REMORK_TOKEN") || got.Code != 6 {
		t.Fatalf("json error = %#v", got)
	}
}

func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return "http://" + addr
}
