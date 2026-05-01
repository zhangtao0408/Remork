package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"remork/internal/api"
	"remork/internal/apply"
	"remork/internal/client"
	"remork/internal/state"
	"remork/internal/watch"
)

type blockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (b *blockingReadCloser) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingReadCloser) Close() error {
	b.once.Do(func() {
		close(b.closed)
	})
	return nil
}

type chunkedSlowReadCloser struct {
	chunks [][]byte
	delay  time.Duration
	closed bool
}

func newChunkedSlowReadCloser(chunks []string, delay time.Duration) *chunkedSlowReadCloser {
	out := make([][]byte, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, []byte(chunk))
	}
	return &chunkedSlowReadCloser{chunks: out, delay: delay}
}

func (r *chunkedSlowReadCloser) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	chunk := r.chunks[0]
	n := copy(p, chunk)
	if n == len(chunk) {
		r.chunks = r.chunks[1:]
	} else {
		r.chunks[0] = chunk[n:]
	}
	return n, nil
}

func (r *chunkedSlowReadCloser) Close() error {
	r.closed = true
	return nil
}

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

func TestStatusReturnsVersionRootsAndThreshold(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Version: "test", Roots: []string{root}, LargeThreshold: 128 << 20}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var status api.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.Version != "test" || len(status.Roots) != 1 || status.Roots[0] != root || status.Threshold != 128<<20 {
		t.Fatalf("bad status: %#v", status)
	}
	if status.Platform == "" {
		t.Fatalf("platform should be populated: %#v", status)
	}
}

func TestTokenProtectedManifestRejectsMissingToken(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}, Token: "secret"}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/manifest?root=" + url.QueryEscape(root) + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestTokenProtectedStatusRequiresBearerToken(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Version: "test", Roots: []string{root}, Token: "secret"}).Handler())
	defer srv.Close()

	for _, tc := range []struct {
		name          string
		authorization string
		want          int
	}{
		{name: "missing", want: http.StatusUnauthorized},
		{name: "wrong", authorization: "Bearer wrong", want: http.StatusUnauthorized},
		{name: "correct", authorization: "Bearer secret", want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
		})
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

func TestApplyEndpointLockedErrorSurfacesThroughClient(t *testing.T) {
	root := t.TempDir()
	lockDir := filepath.Join(root, ".remork", "lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "apply.lock"), []byte("held\n"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	c := client.New(srv.URL)
	result, err := c.Apply(root, apply.Changeset{Changes: []apply.Change{
		{Path: "a.txt", Kind: apply.ChangeCreate, Content: []byte("local")},
	}})
	if err == nil {
		t.Fatal("expected apply lock error")
	}
	if result.Applied {
		t.Fatalf("result applied = true, want false")
	}
	if !strings.Contains(result.Error, "apply lock is already held") {
		t.Fatalf("result error %q missing lock message", result.Error)
	}
	if !strings.Contains(err.Error(), "apply lock is already held") {
		t.Fatalf("client error %q missing lock message; result %#v", err.Error(), result)
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

func TestApplyEndpointTimesOutSlowBodyRead(t *testing.T) {
	root := t.TempDir()
	body := newBlockingReadCloser()
	req := httptest.NewRequest(http.MethodPost, "/apply?root="+url.QueryEscape(root), body)
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		NewServer(Config{
			Roots:                []string{root},
			ApplyBodyReadTimeout: 20 * time.Millisecond,
		}).Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("apply handler did not return after body read timeout")
	}
	if rec.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d; body %q", rec.Code, http.StatusRequestTimeout, rec.Body.String())
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("apply body was not closed on read timeout")
	}
}

func TestApplyEndpointAllowsSlowProgressingBody(t *testing.T) {
	root := t.TempDir()
	body := newChunkedSlowReadCloser([]string{
		`{"changes":[`,
		`{"path":"a.txt","kind":"create","content":"aGVsbG8="}`,
		`]}`,
	}, 15*time.Millisecond)
	req := httptest.NewRequest(http.MethodPost, "/apply?root="+url.QueryEscape(root), body)
	rec := httptest.NewRecorder()
	start := time.Now()

	NewServer(Config{
		Roots:                []string{root},
		ApplyBodyReadTimeout: 20 * time.Millisecond,
	}).Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("test body did not exceed idle timeout wall time: %s", elapsed)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatalf("read applied file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("applied file = %q", data)
	}
}

func TestApplyEndpointReturnsTooLargeForOversizedBody(t *testing.T) {
	root := t.TempDir()
	body := strings.NewReader(`{"changes":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/apply?root="+url.QueryEscape(root), body)
	rec := httptest.NewRecorder()

	NewServer(Config{
		Roots:             []string{root},
		MaxApplyBodyBytes: int64(len(`{"changes":[]}`) - 1),
	}).Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestExecEndpointReturnsTooLargeForOversizedBody(t *testing.T) {
	root := t.TempDir()
	body := strings.NewReader(`{"root":"` + root + `","cwd":"` + root + `","command":["sh","-c","echo ok"]}`)
	req := httptest.NewRequest(http.MethodPost, "/exec", body)
	rec := httptest.NewRecorder()

	NewServer(Config{
		Roots:            []string{root},
		MaxExecBodyBytes: int64(len(`{"root":"` + root + `"`)),
	}).Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body %q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestExecEndpointTimesOutSlowBodyRead(t *testing.T) {
	root := t.TempDir()
	body := newBlockingReadCloser()
	req := httptest.NewRequest(http.MethodPost, "/exec", body)
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		NewServer(Config{
			Roots:               []string{root},
			ExecBodyReadTimeout: 20 * time.Millisecond,
		}).Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("exec handler did not return after body read timeout")
	}
	if rec.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d; body %q", rec.Code, http.StatusRequestTimeout, rec.Body.String())
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("exec body was not closed on read timeout")
	}
}

func TestOperationsEndpointRecordsClientApplyWithoutContent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before\n"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
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
	raw, err := os.ReadFile(filepath.Join(root, ".remork", "log", "operations.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(raw), "YWZ0ZXIK") || strings.Contains(string(raw), "after") {
		t.Fatalf("operation log leaked apply content: %s", raw)
	}
}

func TestShellAcceptsResizeFrame(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	frame := api.ShellFrame{Type: "resize", Rows: 30, Cols: 100}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("printf 'resize-ok\\n'; exit\n")); err != nil {
		t.Fatalf("write command: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var transcript strings.Builder
	for !strings.Contains(transcript.String(), "resize-ok") {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read shell output: %v\ntranscript:\n%s", err, transcript.String())
		}
		transcript.Write(msg)
	}
	_ = conn.Close()
	waitForOperationLogContaining(t, root, `"operation":"shell"`)
}

func TestShellDropsUnknownJSONFrame(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(api.ShellFrame{Type: "bogus", Data: []byte("echo leaked\n")}); err != nil {
		t.Fatalf("unknown frame: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("printf 'done\\n'; exit\n")); err != nil {
		t.Fatalf("write command: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var transcript strings.Builder
	for !strings.Contains(transcript.String(), "done") {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read shell output: %v\ntranscript:\n%s", err, transcript.String())
		}
		transcript.Write(msg)
	}
	if strings.Contains(transcript.String(), "leaked") {
		t.Fatalf("unknown JSON frame was written into shell:\n%s", transcript.String())
	}
	_ = conn.Close()
	waitForOperationLogContaining(t, root, `"operation":"shell"`)
}

func TestShellSendsExitFrame(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("exit 7\n")); err != nil {
		t.Fatalf("write command: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read shell output: %v", err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var frame api.ShellFrame
		if err := json.Unmarshal(msg, &frame); err != nil || frame.Type != "exit" {
			continue
		}
		if frame.ExitCode != 7 {
			t.Fatalf("exit code = %d, want 7", frame.ExitCode)
		}
		_ = conn.Close()
		waitForOperationLogContaining(t, root, `"operation":"shell"`)
		waitForOperationLogContaining(t, root, `"result":"error"`)
		waitForOperationLogContaining(t, root, `"exit_code":7`)
		return
	}
}

func TestShellSessionSurvivesDisconnectAndCanBeKilled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}, Token: "secret"}).Handler())
	defer srv.Close()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer secret")
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("printf 'durable-ready\\n'; sleep 30\n")); err != nil {
		t.Fatalf("write command: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var transcript strings.Builder
	for !strings.Contains(transcript.String(), "durable-ready") {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read shell output: %v\ntranscript:\n%s", err, transcript.String())
		}
		transcript.Write(msg)
	}
	_ = conn.Close()

	var sessions []api.ShellSessionInfo
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/shell/sessions?root="+url.QueryEscape(root), nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("list status %d: %s", resp.StatusCode, body)
		}
		var decoded struct {
			Sessions []api.ShellSessionInfo `json:"sessions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			resp.Body.Close()
			t.Fatalf("decode sessions: %v", err)
		}
		resp.Body.Close()
		if len(decoded.Sessions) > 0 {
			sessions = decoded.Sessions
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v, want one durable session", sessions)
	}
	if sessions[0].ID == "" || len(sessions[0].Command) == 0 || sessions[0].LastActive == "" {
		t.Fatalf("session info missing fields: %#v", sessions[0])
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/shell/sessions?root="+url.QueryEscape(root)+"&id="+url.QueryEscape(sessions[0].ID), nil)
	if err != nil {
		t.Fatalf("new delete request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete status %d: %s", resp.StatusCode, body)
	}
}

func TestShellCanAttachToExistingSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("printf 'ready\\n'\n")); err != nil {
		t.Fatalf("write command: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var transcript strings.Builder
	for !strings.Contains(transcript.String(), "ready") {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read shell output: %v\ntranscript:\n%s", err, transcript.String())
		}
		transcript.Write(msg)
	}
	_ = conn.Close()

	var list struct {
		Sessions []api.ShellSessionInfo `json:"sessions"`
	}
	resp, err := http.Get(srv.URL + "/shell/sessions?root=" + url.QueryEscape(root))
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		resp.Body.Close()
		t.Fatalf("decode sessions: %v", err)
	}
	resp.Body.Close()
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions = %#v, want one", list.Sessions)
	}

	attachURL := wsURL + "&session=" + url.QueryEscape(list.Sessions[0].ID)
	attached, _, err := websocket.DefaultDialer.Dial(attachURL, nil)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer attached.Close()
	if err := attached.WriteMessage(websocket.TextMessage, []byte("printf 'attached\\n'; exit\n")); err != nil {
		t.Fatalf("write attached command: %v", err)
	}
	_ = attached.SetReadDeadline(time.Now().Add(2 * time.Second))
	transcript.Reset()
	for !strings.Contains(transcript.String(), "attached") {
		_, msg, err := attached.ReadMessage()
		if err != nil {
			t.Fatalf("read attached output: %v\ntranscript:\n%s", err, transcript.String())
		}
		transcript.Write(msg)
	}
}

func TestOperationLogsAreScopedPerWorkspaceRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{rootA, rootB}}).Handler())
	defer srv.Close()

	for _, tc := range []struct {
		root     string
		clientID string
	}{
		{root: rootA, clientID: "client-a"},
		{root: rootB, clientID: "client-b"},
	} {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/exec", strings.NewReader(`{"root":"`+tc.root+`","cwd":"`+tc.root+`","command":["sh","-c","pwd"]}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Remork-Client-ID", tc.clientID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}

	rawA, err := os.ReadFile(filepath.Join(rootA, ".remork", "log", "operations.jsonl"))
	if err != nil {
		t.Fatalf("read rootA log: %v", err)
	}
	rawB, err := os.ReadFile(filepath.Join(rootB, ".remork", "log", "operations.jsonl"))
	if err != nil {
		t.Fatalf("read rootB log: %v", err)
	}
	if !strings.Contains(string(rawA), "client-a") || strings.Contains(string(rawA), "client-b") {
		t.Fatalf("rootA log not isolated: %s", rawA)
	}
	if !strings.Contains(string(rawB), "client-b") || strings.Contains(string(rawB), "client-a") {
		t.Fatalf("rootB log not isolated: %s", rawB)
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
	_ = conn.Close()
	waitForOperationLogContaining(t, root, `"operation":"events"`)
}

func TestEventsEndpointStreamsNestedWorkspaceChanges(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "src", "main.txt"), []byte("one\n"))
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/events?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	mustWrite(t, filepath.Join(root, "src", "main.txt"), []byte("two\n"))
	var ev watch.Event
	if err := conn.ReadJSON(&ev); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if ev.Path != "src/main.txt" {
		t.Fatalf("event %#v", ev)
	}
	_ = conn.Close()
	waitForOperationLogContaining(t, root, `"operation":"events"`)
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
			_ = conn.Close()
			waitForOperationLogContaining(t, root, `"operation":"shell"`)
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

func waitForOperationLogContaining(t *testing.T, root, marker string) {
	t.Helper()
	path := filepath.Join(root, ".remork", "log", "operations.jsonl")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(raw), marker) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	raw, _ := os.ReadFile(path)
	t.Fatalf("operation log %s missing marker %q; content: %s", path, marker, raw)
}
