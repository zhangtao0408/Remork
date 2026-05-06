package e2e

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/api"
	"remork/internal/daemon"
)

func TestRemorkProductLargeFilePullPolicies(t *testing.T) {
	h := newProductHarnessWithThreshold(t, 4)
	h.writeRemoteBytes("model.tar.gz", bytes.Repeat([]byte("x"), 8))
	h.bindAndSync()
	if _, err := os.Stat(filepath.Join(h.local, "model.tar.gz.meta")); err != nil {
		t.Fatalf("missing meta: %v", err)
	}
	metaData, err := os.ReadFile(filepath.Join(h.local, "model.tar.gz.meta"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta api.LargeFileMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	fullRefTarget := strings.TrimPrefix(meta.PullCommand, "remork pull ")
	if fullRefTarget == meta.PullCommand {
		t.Fatalf("pull command %q does not start with remork pull", meta.PullCommand)
	}

	out, code := h.runInLocalExpectCode(7, "pull", "--quiet", "model.tar.gz")
	mustContain(t, out, "confirmation required")
	if code != 7 {
		t.Fatalf("exit code = %d", code)
	}
	out, code = h.runInLocalExpectCode(7, "pull", "--non-interactive", "model.tar.gz")
	mustContain(t, out, "confirmation required")
	if code != 7 {
		t.Fatalf("non-interactive exit code = %d", code)
	}
	jsonOut, _, err := h.runInLocalExpectError("pull", "--json", "model.tar.gz")
	if err == nil {
		t.Fatal("pull --json succeeded, want prompt required")
	}
	code = 1
	if coded, ok := err.(interface{ ExitCode() int }); ok {
		code = coded.ExitCode()
	}
	var jsonErr struct {
		Error string `json:"error"`
		Code  int    `json:"code"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &jsonErr); err != nil {
		t.Fatalf("pull --json prompt error is not JSON: %q: %v", jsonOut, err)
	}
	if code != 7 || jsonErr.Code != 7 || !strings.Contains(jsonErr.Error, "confirmation required") {
		t.Fatalf("pull --json error = %#v", jsonErr)
	}

	h.runInLocal("pull", "--force", fullRefTarget)
	got, err := os.ReadFile(filepath.Join(h.local, "model.tar.gz"))
	if err != nil {
		t.Fatalf("read pulled: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("pulled size = %d", len(got))
	}
	if _, err := os.Stat(filepath.Join(h.local, "model.tar.gz.meta")); !os.IsNotExist(err) {
		t.Fatalf("meta still exists after pull: %v", err)
	}

	status := h.runInLocal("status")
	mustContain(t, status, "Large placeholders: 0")
}

func TestRemorkProductPullDirectoryDownloadsChildren(t *testing.T) {
	h := newProductHarnessWithThreshold(t, 128)
	h.writeRemote("src/a.txt", "a\n")
	h.writeRemote("src/nested/b.txt", "b\n")
	h.run("host", "add", "lab", "--url", h.serverURL)
	h.runInLocal("init", "lab:"+h.remote)

	out := h.runInLocal("pull", "src")
	mustContain(t, out, "downloaded 2")
	h.assertLocal("src/a.txt", "a\n")
	h.assertLocal("src/nested/b.txt", "b\n")
}

func newProductHarnessWithThreshold(t *testing.T, threshold int64) *cliHarness {
	t.Helper()
	h := &cliHarness{
		t:      t,
		home:   t.TempDir(),
		local:  t.TempDir(),
		remote: t.TempDir(),
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{h.remote}, LargeThreshold: threshold}).Handler())
	t.Cleanup(srv.Close)
	h.serverURL = srv.URL
	return h
}

func (h *cliHarness) writeRemoteBytes(path string, data []byte) {
	h.t.Helper()
	mustWrite(h.t, filepath.Join(h.remote, path), data)
}
