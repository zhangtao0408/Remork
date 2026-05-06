package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"remork/internal/config"
	execx "remork/internal/exec"
	"remork/internal/workspace"
)

func TestRunRejectsNonPositiveTimeoutBeforeRemoteExec(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "negative", value: "-1s"},
		{name: "zero", value: "0s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			local := t.TempDir()
			stateDir := filepath.Join(home, ".remork", "state", "ws_test")
			if err := workspace.WriteBinding(local, workspace.Binding{
				Version:     1,
				Host:        "lab",
				RemoteRoot:  "/remote/root",
				WorkspaceID: "ws_test",
				StateDir:    stateDir,
			}); err != nil {
				t.Fatalf("write binding: %v", err)
			}

			var execHits int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/exec" {
					http.NotFound(w, r)
					return
				}
				atomic.AddInt32(&execHits, 1)
				_ = json.NewEncoder(w).Encode(execx.Result{Stdout: "should not run\n"})
			}))
			t.Cleanup(server.Close)

			store := config.NewStore(filepath.Join(home, ".remork"))
			if err := store.Save(config.Config{Hosts: map[string]config.Host{
				"lab": {Name: "lab", URL: server.URL},
			}}); err != nil {
				t.Fatalf("save config: %v", err)
			}

			_, _, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "run", "--no-sync-check", "--timeout", tc.value, "--", "echo ignored")
			if err == nil || !strings.Contains(err.Error(), "--timeout must be greater than 0") {
				t.Fatalf("run error = %v, want invalid timeout", err)
			}
			if got := atomic.LoadInt32(&execHits); got != 0 {
				t.Fatalf("/exec was called %d time(s) despite invalid timeout", got)
			}
		})
	}
}

func TestRunCommandWarnsWhenOutputIsTruncated(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	stateDir := filepath.Join(home, ".remork", "state", "ws_test")
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/remote/root",
		WorkspaceID: "ws_test",
		StateDir:    stateDir,
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(execx.Result{
			Stdout:          "partial stdout\n",
			Stderr:          "partial stderr\n",
			StdoutTruncated: true,
			StderrTruncated: true,
		})
	}))
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab": {Name: "lab", URL: server.URL},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local})
	stdout, stderr, err := executeCommandSplit(cmd, "run", "--no-sync-check", "echo ignored")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	mustContain(t, stdout.String(), "partial stdout")
	mustNotContain(t, stdout.String(), "partial stderr")
	mustContain(t, stderr.String(), "partial stderr")
	mustContain(t, stderr.String(), "stdout truncated")
	mustContain(t, stderr.String(), "stderr truncated")
}

func TestRunHelpMentionsOutputReplay(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test"}), "run", "--help")
	if err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(out.String(), "Output is replayed after the remote command completes") {
		t.Fatalf("run help should disclose buffered output behavior, got:\n%s", out.String())
	}
}

func TestRunNonTTYDoesNotPrintProgressPreamble(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	stateDir := filepath.Join(home, ".remork", "state", "ws_test")
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/remote/root",
		WorkspaceID: "ws_test",
		StateDir:    stateDir,
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(execx.Result{Stdout: "done\n"})
	}))
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab": {Name: "lab", URL: server.URL},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "run", "--no-sync-check", "echo ignored")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != "done\n" {
		t.Fatalf("stdout = %q, want remote stdout only", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("non-TTY run should not print progress preamble, got %q", stderr.String())
	}
}
