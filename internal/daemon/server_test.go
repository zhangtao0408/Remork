package daemon

import (
	"encoding/json"
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

func TestOperationsEndpointRecordsClientApplyWithoutContent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before\n"))
	logPath := filepath.Join(t.TempDir(), "operations.jsonl")
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}, OperationLogPath: logPath}).Handler())
	defer srv.Close()

	body := strings.NewReader(`{"changes":[{"path":"a.txt","kind":"update","base_hash":"` + state.HashBytes([]byte("before\n")) + `","content":"YWZ0ZXIK"}]}`)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/apply?root="+url.QueryEscape(root), body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Remork-Client-ID", "tao-macbook")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply status %d", resp.StatusCode)
	}

	opsResp, err := http.Get(srv.URL + "/operations?root=" + url.QueryEscape(root))
	if err != nil {
		t.Fatalf("operations: %v", err)
	}
	defer opsResp.Body.Close()
	var decoded struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(opsResp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("entries: %#v", decoded.Entries)
	}
	entry := decoded.Entries[0]
	if entry["client_id"] != "tao-macbook" || entry["operation"] != "apply" || entry["result"] != "success" {
		t.Fatalf("bad entry: %#v", entry)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(raw), "YWZ0ZXIK") || strings.Contains(string(raw), "after") {
		t.Fatalf("operation log leaked apply content: %s", raw)
	}
}

func TestOperationsEndpointRecordsExecSummary(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/exec", strings.NewReader(`{"root":"`+root+`","cwd":"`+root+`","command":["sh","-c","echo ok"]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Remork-Client-ID", "codex-agent")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	resp.Body.Close()

	opsResp, err := http.Get(srv.URL + "/operations?root=" + url.QueryEscape(root))
	if err != nil {
		t.Fatalf("operations: %v", err)
	}
	defer opsResp.Body.Close()
	var decoded struct {
		Entries []struct {
			ClientID   string         `json:"client_id"`
			Operation  string         `json:"operation"`
			Result     string         `json:"result"`
			StatusCode int            `json:"status_code"`
			Command    []string       `json:"command,omitempty"`
			Summary    map[string]any `json:"request_summary"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(opsResp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("entries: %#v", decoded.Entries)
	}
	entry := decoded.Entries[0]
	if entry.ClientID != "codex-agent" || entry.Operation != "exec" || entry.Result != "success" || entry.StatusCode != http.StatusOK {
		t.Fatalf("bad entry: %#v", entry)
	}
	if len(entry.Command) != 3 || entry.Command[2] != "echo ok" {
		t.Fatalf("command not recorded: %#v", entry)
	}
}

func TestOperationLogRecordsRangeDownloadStatus(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("abcdef"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/download?root="+url.QueryEscape(root)+"&path=a.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=1-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("download status %d", resp.StatusCode)
	}

	opsResp, err := http.Get(srv.URL + "/operations?root=" + url.QueryEscape(root))
	if err != nil {
		t.Fatalf("operations: %v", err)
	}
	defer opsResp.Body.Close()
	var decoded struct {
		Entries []struct {
			Operation  string `json:"operation"`
			StatusCode int    `json:"status_code"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(opsResp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Entries) != 1 || decoded.Entries[0].Operation != "download" || decoded.Entries[0].StatusCode != http.StatusPartialContent {
		t.Fatalf("bad entries: %#v", decoded.Entries)
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
