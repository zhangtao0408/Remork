package e2e

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/cli"
	"remork/internal/daemon"
	"remork/internal/state"
	"remork/internal/workspace"
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

func TestRemorkProductStatusJSON(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "local\n")

	out := h.runInLocal("status", "--json")
	var status struct {
		LocalChanges int `json:"local_changes"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatalf("unmarshal status json: %v\noutput:\n%s", err, out)
	}
	if status.LocalChanges != 1 {
		t.Fatalf("local_changes = %d, want 1; output=%s", status.LocalChanges, out)
	}
}

func TestStatusJSONIncludesEmptyPathLists(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()

	out := h.runInLocal("status", "--json")
	var status map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatalf("unmarshal status json: %v\noutput:\n%s", err, out)
	}
	for _, key := range []string{"changed_paths", "conflict_paths"} {
		raw, ok := status[key]
		if !ok {
			t.Fatalf("status json missing %q; output=%s", key, out)
		}
		if string(raw) != "[]" {
			t.Fatalf("%s = %s, want [] ; output=%s", key, raw, out)
		}
	}
}

func TestRemorkProductStatusTextSummary(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()

	out := h.runInLocal("status")
	for _, want := range []string{
		"Workspace:",
		"Local:",
		"Clean:",
		"Local changes:",
		"Remote updates:",
		"Conflicts:",
		"Large placeholders:",
		"Next:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestRemorkProductDiffAndRestore(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "two\n")

	diffOut := h.runInLocal("diff")
	for _, want := range []string{"-one", "+two"} {
		if !strings.Contains(diffOut, want) {
			t.Fatalf("diff output missing %q:\n%s", want, diffOut)
		}
	}

	h.runInLocal("restore", "a.txt")
	h.assertLocal("a.txt", "one\n")
}

func TestRemorkProductRestoreMissingBaseCacheSuggestsForceSync(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "two\n")
	binding, _, err := workspace.ResolveFrom(h.local)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	basePath, err := state.BasePath(binding.StateDir, "a.txt")
	if err != nil {
		t.Fatalf("base path: %v", err)
	}
	if err := os.Remove(basePath); err != nil {
		t.Fatalf("remove base cache: %v", err)
	}

	_, _, err = h.runInLocalExpectError("restore", "a.txt")
	if err == nil {
		h.t.Fatal("expected restore to fail without base cache")
	}
	if !strings.Contains(err.Error(), "remork sync --force") {
		h.t.Fatalf("restore error = %v, want remork sync --force suggestion", err)
	}
}

func TestRemorkProductRestoreAllKeepsBindingMarker(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.writeLocal("new.txt", "local-only\n")

	h.runInLocal("restore", "--all")

	if _, err := os.Stat(filepath.Join(h.local, workspace.MarkerName)); err != nil {
		t.Fatalf("binding marker missing after restore --all: %v", err)
	}
	if _, _, err := workspace.ResolveFrom(h.local); err != nil {
		t.Fatalf("resolve binding after restore --all: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.local, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("local-only file still exists after restore --all: %v", err)
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

func (h *cliHarness) bindAndSync() {
	h.t.Helper()
	h.run("host", "add", "lab", "--url", h.serverURL)
	h.runInLocal("init", "lab:"+h.remote)
	h.runInLocal("sync")
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
