package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"remork/internal/config"
	execx "remork/internal/exec"
	"remork/internal/output"
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

func TestRunCommandWrapsDefaultCommandWithBashRC(t *testing.T) {
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

	var got execRequestForTest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode exec request: %v", err)
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

	_, _, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "run", "--no-sync-check", "printf \"$REMORK_RC_MARKER\"")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Command) != 3 || got.Command[0] != "bash" || got.Command[1] != "-ic" {
		t.Fatalf("run should execute through interactive bash so ~/.bashrc is loaded, got %#v", got.Command)
	}
	for _, want := range []string{`printf "$REMORK_RC_MARKER"`} {
		if !strings.Contains(got.Command[2], want) {
			t.Fatalf("wrapped command should contain %q, got %q", want, got.Command[2])
		}
	}
}

func TestRunCommandShellQuotesArgvWhenSourcingBashRC(t *testing.T) {
	got := runCommandArgs([]string{"python", "-c", `print("hello world")`})
	if len(got) != 3 || got[0] != "bash" || got[1] != "-ic" {
		t.Fatalf("runCommandArgs = %#v, want bash wrapper", got)
	}
	if !strings.Contains(got[2], `'python' '-c' 'print("hello world")'`) {
		t.Fatalf("wrapped argv should be shell quoted, got %q", got[2])
	}
}

func TestRunCommandFiltersInteractiveBashStartupWarnings(t *testing.T) {
	got := cleanRunStderr("bash: cannot set terminal process group (-1): Inappropriate ioctl for device\nbash: no job control in this shell\nreal stderr\n")
	if got != "real stderr\n" {
		t.Fatalf("cleaned stderr = %q", got)
	}
}

func TestRunProgressUsesLiveSpinner(t *testing.T) {
	var buf lockedBuffer
	reporter := newRunProgress(&buf, output.ColorNever)
	waitForRunOutput(t, &buf, "o remote command running; output is replayed after completion...")
	reporter.DoneMessage("remote command output ready")

	out := buf.String()
	if strings.Contains(out, "->") {
		t.Fatalf("run progress should use spinner frames, got %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("run progress should rewrite one line and finish with one newline, got %q", out)
	}
	if !strings.Contains(out, "ok remote command output ready") {
		t.Fatalf("run progress should finish with ok, got %q", out)
	}
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

type execRequestForTest struct {
	Command []string `json:"command"`
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForRunOutput(t *testing.T, buf *lockedBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", want, buf.String())
}
