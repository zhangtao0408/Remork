package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/api"
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

func TestClientSendsClientIDAndReadsOperations(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	c := NewWithClientID(srv.URL, "codex-agent")
	if _, err := c.Exec(root, root, []string{"sh", "-c", "echo hello"}, 0); err != nil {
		t.Fatalf("exec: %v", err)
	}
	entries, err := c.Operations(root, 10)
	if err != nil {
		t.Fatalf("operations: %v", err)
	}
	if len(entries) != 1 || entries[0].ClientID != "codex-agent" || entries[0].Operation != "exec" {
		t.Fatalf("bad entries: %#v", entries)
	}
}

func TestClientSendsClientIDAndBearerToken(t *testing.T) {
	var sawStatus bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		sawStatus = true
		if got := r.Header.Get(api.HeaderClientID); got != "tao-macbook" {
			t.Errorf("%s = %q, want tao-macbook", api.HeaderClientID, got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer abc123" {
			t.Errorf("Authorization = %q, want Bearer abc123", got)
		}
		if err := json.NewEncoder(w).Encode(api.StatusResponse{Version: "test"}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer srv.Close()

	c := NewWithOptions(Options{BaseURL: srv.URL, ClientID: "tao-macbook", Token: "abc123"})
	status, err := c.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !sawStatus {
		t.Fatal("status endpoint was not called")
	}
	if status.Version != "test" {
		t.Fatalf("status: %#v", status)
	}
}

func TestClientTrimsTrailingSlashFromBaseURL(t *testing.T) {
	var sawStatus bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("path = %q, want /status", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		sawStatus = true
		if err := json.NewEncoder(w).Encode(api.StatusResponse{Version: "test"}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer srv.Close()

	c := NewWithOptions(Options{BaseURL: srv.URL + "/"})
	if _, err := c.Status(); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !sawStatus {
		t.Fatal("status endpoint was not called")
	}
}

func TestClientReturnsHTTPErrorForUnavailableDaemon(t *testing.T) {
	c := New("http://127.0.0.1:1")
	_, err := c.Manifest("/tmp/missing", ".")
	if err == nil {
		t.Fatal("expected connection error")
	}
}
