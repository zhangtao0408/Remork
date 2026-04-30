package e2e

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/cli"
	"remork/internal/daemon"
)

func TestRemorkProductSyncFromBoundDirectory(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("src/main.txt", "hello from remote")

	out := h.run("host", "add", "lab", "--url", h.serverURL)
	if out != "" {
		t.Fatalf("host add output = %q, want empty", out)
	}
	h.runInLocal("init", "lab:"+h.remote)
	syncOut := h.runInLocal("sync")
	if !bytes.Contains([]byte(syncOut), []byte("downloaded 1")) {
		t.Fatalf("sync output %q does not contain downloaded 1", syncOut)
	}
	h.assertLocal("src/main.txt", "hello from remote")
}

func TestRemorkProductSyncJSONConflictDoesNotPrintSuccess(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "base")
	h.run("host", "add", "lab", "--url", h.serverURL)
	h.runInLocal("init", "lab:"+h.remote)
	h.runInLocal("sync")
	h.writeLocal("a.txt", "local-dirty")
	h.writeRemote("a.txt", "remote-new")

	stdout, _, err := h.runInLocalExpectError("sync", "--json")
	if err == nil {
		h.t.Fatal("expected sync conflict error")
	}
	if stdout != "" {
		h.t.Fatalf("stdout = %q, want empty", stdout)
	}
}

type cliHarness struct {
	t         *testing.T
	home      string
	local     string
	remote    string
	serverURL string
}

func newCLIHarness(t *testing.T) *cliHarness {
	t.Helper()
	h := &cliHarness{
		t:      t,
		home:   t.TempDir(),
		local:  t.TempDir(),
		remote: t.TempDir(),
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{h.remote}}).Handler())
	t.Cleanup(srv.Close)
	h.serverURL = srv.URL
	return h
}

func (h *cliHarness) run(args ...string) string {
	h.t.Helper()
	return h.runWithWorkingDir(h.home, args...)
}

func (h *cliHarness) runInLocal(args ...string) string {
	h.t.Helper()
	return h.runWithWorkingDir(h.local, args...)
}

func (h *cliHarness) runInLocalExpectError(args ...string) (string, string, error) {
	h.t.Helper()
	cmd := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: h.home, WorkingDir: h.local})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func (h *cliHarness) runWithWorkingDir(workingDir string, args ...string) string {
	h.t.Helper()
	cmd := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: h.home, WorkingDir: workingDir})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		h.t.Fatalf("remork %v: %v\noutput:\n%s", args, err, out.String())
	}
	return out.String()
}

func (h *cliHarness) writeRemote(path, content string) {
	h.t.Helper()
	mustWrite(h.t, filepath.Join(h.remote, path), []byte(content))
}

func (h *cliHarness) writeLocal(path, content string) {
	h.t.Helper()
	mustWrite(h.t, filepath.Join(h.local, path), []byte(content))
}

func (h *cliHarness) assertLocal(path, want string) {
	h.t.Helper()
	got, err := os.ReadFile(filepath.Join(h.local, path))
	if err != nil {
		h.t.Fatalf("read local %s: %v", path, err)
	}
	if string(got) != want {
		h.t.Fatalf("local %s = %q, want %q", path, got, want)
	}
}
