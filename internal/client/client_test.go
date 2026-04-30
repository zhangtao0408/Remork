package client

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/daemon"
)

func TestClientManifestAndDownload(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := New(srv.URL)
	manifest, err := c.Manifest(root, ".")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("empty manifest")
	}
	data, err := c.Download(root, "a.txt")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("data %q", data)
	}
}

func TestClientDownloadRange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := New(srv.URL)
	data, err := c.DownloadRange(root, "a.txt", 2, 4)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if string(data) != "cde" {
		t.Fatalf("data %q", data)
	}
}

func TestClientReturnsHTTPErrorForUnavailableDaemon(t *testing.T) {
	c := New("http://127.0.0.1:1")
	_, err := c.Manifest("/tmp/missing", ".")
	if err == nil {
		t.Fatal("expected connection error")
	}
}
