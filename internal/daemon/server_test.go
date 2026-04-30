package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"remork/internal/state"
	"remork/internal/watch"
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

func TestApplyEndpointConflictReturnsConflict(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("remote"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	body := strings.NewReader(`{"changes":[{"path":"a.txt","kind":"update","base_hash":"` + state.HashBytes([]byte("base")) + `","content":"bG9jYWw="}]}`)
	resp, err := http.Post(srv.URL+"/apply?root="+url.QueryEscape(root), "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestApplyEndpointInvalidPathReturnsBadRequest(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	body := strings.NewReader(`{"changes":[{"path":"../escape","kind":"update","base_hash":"sha256:nope","content":"bG9jYWw="}]}`)
	resp, err := http.Post(srv.URL+"/apply?root="+url.QueryEscape(root), "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestExecEndpointRunsCommand(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	body := strings.NewReader(`{"root":"` + root + `","cwd":"` + root + `","command":["sh","-c","pwd"]}`)
	resp, err := http.Post(srv.URL+"/exec", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(data), root) {
		t.Fatalf("body: %s", data)
	}
}

func TestExecEndpointRejectsCwdEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	body := strings.NewReader(`{"root":"` + root + `","cwd":"` + outside + `","command":["sh","-c","pwd"]}`)
	resp, err := http.Post(srv.URL+"/exec", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestEventsEndpointStreamsWorkspaceChanges(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/events?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	mustWrite(t, filepath.Join(root, "watched.txt"), []byte("hello"))
	var ev watch.Event
	if err := conn.ReadJSON(&ev); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if ev.Path != "watched.txt" {
		t.Fatalf("event %#v", ev)
	}
}

func TestShellEndpointRunsInteractiveCommand(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("echo shell-ok\nexit\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var out strings.Builder
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			continue
		}
		out.Write(msg)
		if strings.Contains(out.String(), "shell-ok") {
			return
		}
	}
	t.Fatalf("shell output missing marker: %q", out.String())
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
