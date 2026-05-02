package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"remork/internal/cli"
	"remork/internal/config"
	"remork/internal/daemon"
	"remork/internal/state"
	"remork/internal/workspace"
)

const productTestClientID = "tao-test"

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

func TestRemorkProductFullWorkflow(t *testing.T) {
	h := newProductHarnessWithThreshold(t, 32)
	h.writeRemote("src/main.txt", "hello from remote\n")
	h.writeRemoteBytes("model.tar.gz", bytes.Repeat([]byte("x"), 64))

	out := h.run("host", "add", "lab", "--url", h.serverURL)
	if out != "" {
		t.Fatalf("host add output = %q, want empty", out)
	}
	h.runInLocal("init", "lab:"+h.remote)

	syncOut := h.runInLocal("sync")
	mustContain(t, syncOut, "downloaded 1")
	mustContain(t, syncOut, "meta 1")
	h.assertLocal("src/main.txt", "hello from remote\n")
	if _, err := os.Stat(filepath.Join(h.local, "model.tar.gz.meta")); err != nil {
		t.Fatalf("missing large-file placeholder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.local, "model.tar.gz")); !os.IsNotExist(err) {
		t.Fatalf("large file materialized during sync: %v", err)
	}

	h.writeLocal("src/main.txt", "hello from local\n")
	statusOut := h.runInLocal("status")
	mustContain(t, statusOut, "Local changes: 1")
	mustContain(t, statusOut, "Large placeholders: 1")

	diffOut := h.runInLocal("diff")
	mustContain(t, diffOut, "-hello from remote")
	mustContain(t, diffOut, "+hello from local")

	applyOut := h.runInLocal("apply")
	mustContain(t, applyOut, "applied 1")
	h.assertRemote("src/main.txt", "hello from local\n")

	runOut := h.runInLocal("run", "cat src/main.txt")
	mustContain(t, runOut, "hello from local")

	pullOut := h.runInLocal("pull", "--force", "model.tar.gz")
	mustContain(t, pullOut, "downloaded 1")
	modelData, err := os.ReadFile(filepath.Join(h.local, "model.tar.gz"))
	if err != nil {
		t.Fatalf("read pulled large file: %v", err)
	}
	if len(modelData) != 64 {
		t.Fatalf("pulled large file size = %d, want 64", len(modelData))
	}
	if _, err := os.Stat(filepath.Join(h.local, "model.tar.gz.meta")); !os.IsNotExist(err) {
		t.Fatalf("large-file placeholder still exists after force pull: %v", err)
	}

	logOut := h.runInLocal("log", "--limit", "20")
	mustContain(t, logOut, "operation")
	mustContain(t, logOut, "apply")
	mustContain(t, logOut, "run")

	logJSON := h.runInLocal("log", "--limit", "20", "--json")
	var entries []map[string]any
	if err := json.Unmarshal([]byte(logJSON), &entries); err != nil {
		t.Fatalf("unmarshal log json: %v\noutput:\n%s", err, logJSON)
	}
	var sawApply, sawExec bool
	for _, entry := range entries {
		switch entry["operation"] {
		case "apply":
			sawApply = true
		case "exec":
			sawExec = true
		}
	}
	if !sawApply || !sawExec {
		t.Fatalf("operation log missing apply/exec entries: %#v", entries)
	}

	rawLog, err := os.ReadFile(filepath.Join(h.remote, ".remork", "log", "operations.jsonl"))
	if err != nil {
		t.Fatalf("read remote operation log: %v", err)
	}
	mustContain(t, string(rawLog), `"operation":"apply"`)
	mustContain(t, string(rawLog), `"operation":"exec"`)
}

func TestRemorkApplyExplicitPathPreservesOtherDirtyLocalFiles(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "remote-a\n")
	h.writeRemote("b.txt", "remote-b\n")
	h.bindAndSync()

	h.writeLocal("a.txt", "local-a\n")
	h.writeLocal("b.txt", "local-b\n")

	applyOut := h.runInLocal("apply", "a.txt")
	mustContain(t, applyOut, "applied 1")
	h.assertRemote("a.txt", "local-a\n")
	h.assertRemote("b.txt", "remote-b\n")
	h.assertLocal("a.txt", "local-a\n")
	h.assertLocal("b.txt", "local-b\n")
}

func TestRemorkApplyExplicitDeletePreservesOtherDirtyLocalFiles(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "remote-a\n")
	h.writeRemote("b.txt", "remote-b\n")
	h.bindAndSync()

	if err := os.Remove(filepath.Join(h.local, "a.txt")); err != nil {
		t.Fatalf("remove local a.txt: %v", err)
	}
	h.writeLocal("b.txt", "local-b\n")

	applyOut := h.runInLocal("apply", "a.txt")
	mustContain(t, applyOut, "applied 1")
	if _, err := os.Stat(filepath.Join(h.remote, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("remote a.txt still exists after apply delete: %v", err)
	}
	h.assertRemote("b.txt", "remote-b\n")
	h.assertLocal("b.txt", "local-b\n")

	statusOut := h.runInLocal("status")
	mustContain(t, statusOut, "Local changes: 1")
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

func TestRemorkProductDiffAndRestoreDashPrefixedPath(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("-dash.txt", "one\n")
	h.bindAndSync()
	h.writeLocal("-dash.txt", "two\n")

	diffOut := h.runInLocal("diff", "--", "-dash.txt")
	for _, want := range []string{"-one", "+two"} {
		if !strings.Contains(diffOut, want) {
			t.Fatalf("diff output missing %q:\n%s", want, diffOut)
		}
	}

	h.runInLocal("restore", "--", "-dash.txt")
	h.assertLocal("-dash.txt", "one\n")
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

func TestRemorkProductRunSafeMode(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "remote\n")
	h.bindAndSync()
	out := h.runInLocal("run", "cat a.txt")
	mustContain(t, out, "remote")

	h.writeLocal("a.txt", "local\n")
	blocked, code := h.runInLocalExpectCode(4, "run", "cat a.txt")
	mustContain(t, blocked, "Local changes exist")
	if code != 4 {
		t.Fatalf("exit code = %d", code)
	}

	remoteOnly := h.runInLocal("run", "--remote-only", "cat a.txt")
	mustContain(t, remoteOnly, "remote")
	mustContain(t, remoteOnly, "local pending changes are ignored")
}

func TestRemorkProductRunSyncsCleanStaleWorkspace(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "remote-one\n")
	h.bindAndSync()
	h.writeRemote("a.txt", "remote-two\n")

	out := h.runInLocal("run", "cat a.txt")
	mustContain(t, out, "remote-two")
	h.assertLocal("a.txt", "remote-two\n")
}

func TestRemorkProductLogShowsWorkspaceOperations(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.runInLocal("run", "cat a.txt")

	out := h.runInLocal("log", "--limit", "5")

	mustContain(t, out, "time")
	mustContain(t, out, "client")
	mustContain(t, out, "operation")
	mustContain(t, out, "result")
	mustContain(t, out, "summary")
	mustContain(t, out, "run")
	mustContain(t, out, "cat a.txt")
	mustContain(t, out, productTestClientID)

	jsonOut := h.runInLocal("log", "--limit", "5", "--json")
	var entries []map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &entries); err != nil {
		t.Fatalf("unmarshal log json: %v\noutput:\n%s", err, jsonOut)
	}
	if len(entries) == 0 {
		t.Fatalf("log json has no entries: %s", jsonOut)
	}
	var sawExec bool
	for _, entry := range entries {
		if entry["operation"] == "exec" && entry["client_id"] == productTestClientID {
			sawExec = true
		}
	}
	if !sawExec {
		t.Fatalf("log json missing raw exec entry for %s: %#v", productTestClientID, entries)
	}
}

func TestRemorkProductDoctorReportsReady(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()

	out := h.runInLocal("doctor")

	mustContain(t, out, "OK: workspace is ready")
}

func TestRemorkProductDoctorReportsUnboundFailure(t *testing.T) {
	h := newProductHarness(t)
	h.run("host", "add", "lab", "--url", h.serverURL)

	out, code := h.runInLocalExpectCode(2, "doctor")

	mustContain(t, out, "FAILED:")
	mustContain(t, out, "Fix:")
	mustContain(t, out, "remork init")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestRemorkProductDebugManifestAndAPI(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()

	manifestOut := h.runInLocal("debug", "manifest")
	mustContain(t, manifestOut, "entries:")
	mustContain(t, manifestOut, "a.txt")

	manifestJSON := h.runInLocal("debug", "manifest", "--json")
	var decodedManifest map[string]any
	if err := json.Unmarshal([]byte(manifestJSON), &decodedManifest); err != nil {
		t.Fatalf("unmarshal debug manifest json: %v\noutput:\n%s", err, manifestJSON)
	}
	if decodedManifest["root"] != h.remote {
		t.Fatalf("manifest root = %#v, want %q", decodedManifest["root"], h.remote)
	}

	apiOut := h.runInLocal("debug", "api")
	mustContain(t, apiOut, "status OK")
	mustContain(t, apiOut, "manifest OK")
	mustContain(t, apiOut, "operations OK")
}

func TestRemorkProductWatchStreamsNestedRemoteEvents(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("src/main.txt", "one\n")
	h.bindAndSync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	cmd := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: h.home, WorkingDir: h.local})
	cmd.SetContext(ctx)
	cmd.SetOut(writer)
	cmd.SetErr(writer)
	cmd.SetArgs([]string{"watch"})
	errCh := make(chan error, 1)
	go func() {
		err := cmd.Execute()
		_ = writer.Close()
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		errCh <- err
	}()

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	waitForLine(t, lines, "watching")
	h.writeRemote("src/main.txt", "two\n")
	waitForLine(t, lines, "src/main.txt")
	waitForLocalContent(t, filepath.Join(h.local, "src", "main.txt"), "two\n")
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("watch returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop after context cancellation")
	}
	waitForFileContaining(t, filepath.Join(h.remote, ".remork", "log", "operations.jsonl"), `"operation":"events"`)
}

func TestRemorkProductWatchDebouncesBurstRemoteEvents(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("seed.txt", "seed\n")
	h.bindAndSync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	cmd := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: h.home, WorkingDir: h.local})
	cmd.SetContext(ctx)
	cmd.SetOut(writer)
	cmd.SetErr(writer)
	cmd.SetArgs([]string{"watch", "--debounce", "50ms", "--reconcile-interval", "150ms"})
	errCh := make(chan error, 1)
	go func() {
		err := cmd.Execute()
		_ = writer.Close()
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		errCh <- err
	}()

	lines := make(chan string, 128)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	waitForLine(t, lines, "watching")
	wantBurst := make(map[string]string, 12)
	for i := 0; i < 12; i++ {
		name := filepath.Join("burst", fmt.Sprintf("file-%02d.txt", i))
		content := fmt.Sprintf("remote-%02d\n", i)
		wantBurst[name] = content
		h.writeRemote(name, content)
	}
	waitForLocalContents(t, h.local, wantBurst)
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("watch returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop after context cancellation")
	}
	waitForFileContaining(t, filepath.Join(h.remote, ".remork", "log", "operations.jsonl"), `"operation":"events"`)
}

func waitForLine(t *testing.T, lines <-chan string, want string) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("output closed before seeing %q", want)
			}
			if strings.Contains(line, want) {
				return line
			}
		case <-deadline:
			t.Fatalf("timed out waiting for output containing %q", want)
		}
	}
}

func waitForLocalContent(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && string(data) == want {
			return
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("timed out waiting for %s to contain %q: %v", path, want, err)
			}
			t.Fatalf("timed out waiting for %s to equal %q; got %q", path, want, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForLocalContents(t *testing.T, root string, wants map[string]string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var missing []string
		var mismatched []string
		for path, want := range wants {
			fullPath := filepath.Join(root, path)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				missing = append(missing, path)
				continue
			}
			if string(data) != want {
				mismatched = append(mismatched, fmt.Sprintf("%s=%q want %q", path, data, want))
			}
		}
		if len(missing) == 0 && len(mismatched) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for local contents; missing=%v mismatched=%v", missing, mismatched)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForFileContaining(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("timed out waiting for %s to contain %q: %v", path, want, err)
			}
			t.Fatalf("timed out waiting for %s to contain %q", path, want)
		}
		time.Sleep(10 * time.Millisecond)
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
	remote, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve remote temp dir: %v", err)
	}
	h := &cliHarness{
		t:      t,
		home:   t.TempDir(),
		local:  t.TempDir(),
		remote: remote,
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{h.remote}}).Handler())
	t.Cleanup(srv.Close)
	h.serverURL = srv.URL
	store := config.NewStore(filepath.Join(h.home, ".remork"))
	if err := store.Save(config.Config{
		ClientID:   productTestClientID,
		Hosts:      map[string]config.Host{},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("seed test config: %v", err)
	}
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
