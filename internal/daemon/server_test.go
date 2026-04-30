package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestEndpointReturnsEntries(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("hello"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}, LargeThreshold: 128 << 20}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest?root=" + root + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "a.txt") {
		t.Fatalf("body missing a.txt: %s", body)
	}
}

func TestDownloadRejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/download?root=" + root + "&path=../secret")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestDownloadSupportsRange(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("abcdef"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/download?root="+root+"&path=a.txt", nil)
	req.Header.Set("Range", "bytes=1-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "bcd" {
		t.Fatalf("range body %q", body)
	}
}

func TestManifestUnknownRootReturnsForbidden(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/manifest?root=" + other + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestDownloadEncodedTraversalReturnsBadRequest(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/download?root=" + root + "&path=%2e%2e/escape")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestDownloadSymlinkEscapeReturnsBadRequest(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.txt"), []byte("secret"))
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/download?root=" + root + "&path=link.txt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
