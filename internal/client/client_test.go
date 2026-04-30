package client

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/apply"
	"remork/internal/daemon"
	"remork/internal/state"
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

func TestClientApplyUpdate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := New(srv.URL)
	res, err := c.Apply(root, apply.Changeset{Changes: []apply.Change{
		{Path: "a.txt", Kind: apply.ChangeUpdate, BaseHash: state.HashBytes([]byte("before")), Content: []byte("after")},
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied {
		t.Fatalf("not applied: %#v", res)
	}
}

func TestClientExecRunsCommand(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := New(srv.URL)
	res, err := c.Exec(root, root, []string{"sh", "-c", "echo hello"}, 0)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.ExitCode != 0 || res.Stdout != "hello\n" {
		t.Fatalf("bad result: %#v", res)
	}
}

func TestClientReturnsHTTPErrorForUnavailableDaemon(t *testing.T) {
	c := New("http://127.0.0.1:1")
	_, err := c.Manifest("/tmp/missing", ".")
	if err == nil {
		t.Fatal("expected connection error")
	}
}
