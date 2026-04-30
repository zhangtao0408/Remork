package e2e

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/daemon"
)

func TestRemorkProductLargeFilePullPolicies(t *testing.T) {
	h := newProductHarnessWithThreshold(t, 4)
	h.writeRemoteBytes("model.tar.gz", bytes.Repeat([]byte("x"), 8))
	h.bindAndSync()
	if _, err := os.Stat(filepath.Join(h.local, "model.tar.gz.meta")); err != nil {
		t.Fatalf("missing meta: %v", err)
	}

	out, code := h.runInLocalExpectCode(7, "pull", "--quiet", "model.tar.gz")
	mustContain(t, out, "confirmation required")
	if code != 7 {
		t.Fatalf("exit code = %d", code)
	}

	h.runInLocal("pull", "--force", "model.tar.gz")
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
