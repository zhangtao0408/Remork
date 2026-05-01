package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"remork/internal/api"
	"remork/internal/apply"
	"remork/internal/daemon"
	execx "remork/internal/exec"
	"remork/internal/limits"
	"remork/internal/state"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestClientApplyBoundsNonJSONErrorBody(t *testing.T) {
	const sentinel = "SHOULD_NOT_APPEAR"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apply" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", limits.MaxErrorBodyBytes) + sentinel))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.Apply("/remote/root", apply.Changeset{})
	if err == nil {
		t.Fatal("expected apply error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T %v, want *HTTPError", err, err)
	}
	if strings.Contains(httpErr.Body, sentinel) {
		t.Fatalf("error body was not bounded: %q", httpErr.Body)
	}
}

func TestClientApplyPreservesStructuredErrorResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apply" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusConflict)
		if err := json.NewEncoder(w).Encode(apply.Result{Applied: false, Conflicts: []string{"a.txt"}}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer srv.Close()

	c := New(srv.URL)
	res, err := c.Apply("/remote/root", apply.Changeset{})
	if err == nil {
		t.Fatal("expected apply conflict error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T %v, want *HTTPError", err, err)
	}
	if httpErr.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", httpErr.StatusCode, http.StatusConflict)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0] != "a.txt" {
		t.Fatalf("result = %#v, want conflict result", res)
	}
}

func TestClientApplyPreservesLargeStructuredErrorResult(t *testing.T) {
	conflictName := strings.Repeat("conflict-file-", limits.MaxErrorBodyBytes/len("conflict-file-")+1)
	result := apply.Result{Applied: false, Conflicts: []string{conflictName}}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(body) <= limits.MaxErrorBodyBytes {
		t.Fatalf("test body = %d bytes, want > %d", len(body), limits.MaxErrorBodyBytes)
	}
	if len(body) >= limits.MaxApplyResultBodyBytes {
		t.Fatalf("test body = %d bytes, want < %d", len(body), limits.MaxApplyResultBodyBytes)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apply" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := New(srv.URL)
	res, err := c.Apply("/remote/root", apply.Changeset{})
	if err == nil {
		t.Fatal("expected apply conflict error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T %v, want *HTTPError", err, err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0] != conflictName {
		t.Fatalf("result conflict was not preserved: %#v", res)
	}
	if len(httpErr.Body) > limits.MaxErrorBodyBytes {
		t.Fatalf("HTTPError.Body length = %d, want <= %d", len(httpErr.Body), limits.MaxErrorBodyBytes)
	}
}

func TestClientDownloadContextCancellationReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("partial"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		c := New(srv.URL)
		_, err := c.DownloadContext(ctx, "/remote/root", "large.bin")
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("download did not return promptly after context cancellation")
	}
}

func TestClientApplyContextCancellationReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apply" {
			http.NotFound(w, r)
			return
		}
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		c := New(srv.URL)
		_, err := c.ApplyContext(ctx, "/remote/root", apply.Changeset{})
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("apply did not return promptly after context cancellation")
	}
}

func TestClientManifestContextCancellationReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manifest" {
			http.NotFound(w, r)
			return
		}
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		c := New(srv.URL)
		_, err := c.ManifestContext(ctx, "/remote/root", ".")
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("manifest did not return promptly after context cancellation")
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

func TestClientNoProxyDisablesProxyFromEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	c := NewWithOptions(Options{BaseURL: "http://example.test", NoProxy: true})

	transport, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.http.Transport)
	}
	if transport.Proxy != nil {
		req, err := http.NewRequest(http.MethodGet, "http://example.test/status", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		proxyURL, err := transport.Proxy(req)
		if err != nil {
			t.Fatalf("proxy lookup: %v", err)
		}
		if proxyURL != nil {
			t.Fatalf("proxy = %v, want nil", proxyURL)
		}
	}
}

func TestClientReturnsHTTPErrorForUnavailableDaemon(t *testing.T) {
	c := New("http://127.0.0.1:1")
	_, err := c.Manifest("/tmp/missing", ".")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestNewHTTPClientHasDefaultTimeout(t *testing.T) {
	c := NewHTTPClient(false)
	if c.Timeout != limits.DefaultHTTPTimeout {
		t.Fatalf("timeout = %s, want %s", c.Timeout, limits.DefaultHTTPTimeout)
	}
	transport, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.Transport)
	}
	if transport.ResponseHeaderTimeout != limits.DefaultHTTPTimeout {
		t.Fatalf("response header timeout = %s, want %s", transport.ResponseHeaderTimeout, limits.DefaultHTTPTimeout)
	}
}

func TestNoProxyHTTPClientKeepsDefaultTimeout(t *testing.T) {
	c := NewHTTPClient(true)
	if c.Timeout != limits.DefaultHTTPTimeout {
		t.Fatalf("timeout = %s, want %s", c.Timeout, limits.DefaultHTTPTimeout)
	}
	transport, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.Transport)
	}
	if transport.ResponseHeaderTimeout != limits.DefaultHTTPTimeout {
		t.Fatalf("response header timeout = %s, want %s", transport.ResponseHeaderTimeout, limits.DefaultHTTPTimeout)
	}
}

func TestClientDownloadIgnoresWholeRequestTimeoutForSlowBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			http.NotFound(w, r)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("slow body"))
	}))
	defer srv.Close()

	c := NewWithOptions(Options{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Millisecond}})
	data, err := c.Download("/remote/root", "large.bin")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != "slow body" {
		t.Fatalf("data = %q, want slow body", data)
	}
}

func TestClientDownloadContextUsesDefaultDeadlineWhenCallerHasNone(t *testing.T) {
	c := NewWithOptions(Options{
		BaseURL: "http://example.test",
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			if !ok {
				t.Fatal("request context has no deadline")
			}
			if remaining := time.Until(deadline); remaining <= 0 || remaining > limits.DefaultTransferTimeout {
				t.Fatalf("deadline remaining = %s, want within %s", remaining, limits.DefaultTransferTimeout)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("download body")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
	})

	data, err := c.DownloadContext(context.Background(), "/remote/root", "large.bin")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != "download body" {
		t.Fatalf("data = %q", data)
	}
}

func TestClientApplyContextUsesDefaultDeadlineWhenCallerHasNone(t *testing.T) {
	c := NewWithOptions(Options{
		BaseURL: "http://example.test",
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			if !ok {
				t.Fatal("request context has no deadline")
			}
			if remaining := time.Until(deadline); remaining <= 0 || remaining > limits.DefaultApplyTimeout {
				t.Fatalf("deadline remaining = %s, want within %s", remaining, limits.DefaultApplyTimeout)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"applied":true}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
	})

	res, err := c.ApplyContext(context.Background(), "/remote/root", apply.Changeset{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied {
		t.Fatalf("result = %#v, want applied", res)
	}
}

func TestClientExecContextClearsResponseHeaderTimeoutForLongOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(50 * time.Millisecond)
		if err := json.NewEncoder(w).Encode(execx.Result{Stdout: "done\n"}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer srv.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Millisecond
	c := NewWithOptions(Options{BaseURL: srv.URL, HTTP: &http.Client{Transport: transport}})
	res, err := c.ExecContext(context.Background(), "/remote/root", "/remote/root", []string{"sh", "-c", "echo done"}, 1000)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.Stdout != "done\n" {
		t.Fatalf("stdout = %q, want done", res.Stdout)
	}
}

func TestClientDownloadContextClearsResponseHeaderTimeoutForLongOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("slow header body"))
	}))
	defer srv.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Millisecond
	c := NewWithOptions(Options{BaseURL: srv.URL, HTTP: &http.Client{Transport: transport}})
	data, err := c.DownloadContext(context.Background(), "/remote/root", "large.bin")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != "slow header body" {
		t.Fatalf("data = %q", data)
	}
}

func TestClientExecUsesOperationTimeoutInsteadOfWholeRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(50 * time.Millisecond)
		if err := json.NewEncoder(w).Encode(execx.Result{Stdout: "done\n"}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer srv.Close()

	c := NewWithOptions(Options{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Millisecond}})
	res, err := c.Exec("/remote/root", "/remote/root", []string{"sh", "-c", "echo done"}, 1000)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.Stdout != "done\n" {
		t.Fatalf("stdout = %q, want done", res.Stdout)
	}
}
