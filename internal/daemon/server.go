package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"remork/internal/api"
	"remork/internal/apply"
	"remork/internal/auth"
	execx "remork/internal/exec"
	"remork/internal/limits"
	"remork/internal/manifest"
	"remork/internal/ops"
	"remork/internal/paths"
	ptysession "remork/internal/pty"
	"remork/internal/remoteroot"
	"remork/internal/securefs"
	"remork/internal/watch"
)

type Config struct {
	Version              string
	Roots                []string
	LargeThreshold       int64
	Token                string
	ApplyBodyReadTimeout time.Duration
	ExecBodyReadTimeout  time.Duration
	MaxApplyBodyBytes    int64
	MaxExecBodyBytes     int64
}

type Server struct {
	cfg             Config
	mux             *http.ServeMux
	ptyManager      *ptysession.Manager
	allowedRoots    []remoteroot.Root
	operationMu     sync.Mutex
	operationStores map[string]ops.Store
}

func NewServer(cfg Config) *Server {
	normalizedRoots, err := normalizeConfiguredRoots(cfg.Roots)
	if err != nil {
		normalizedRoots = nil
	}
	cfg.Roots = normalizedRoots
	allowedRoots, err := remoteroot.NormalizeMany(normalizedRoots)
	if err != nil {
		allowedRoots = nil
	}
	s := &Server{
		cfg:             cfg,
		mux:             http.NewServeMux(),
		ptyManager:      ptysession.NewManager(30 * time.Minute),
		allowedRoots:    allowedRoots,
		operationStores: map[string]ops.Store{},
	}
	s.mux.HandleFunc("/status", s.withAuth(s.handleStatus))
	s.mux.HandleFunc("/manifest", s.withAuth(s.handleManifest))
	s.mux.HandleFunc("/download", s.withAuth(s.handleDownload))
	s.mux.HandleFunc("/apply", s.withAuth(s.handleApply))
	s.mux.HandleFunc("/exec", s.withAuth(s.handleExec))
	s.mux.HandleFunc("/events", s.withAuth(s.handleEvents))
	s.mux.HandleFunc("/shell", s.withAuth(s.handleShell))
	s.mux.HandleFunc("/shell/sessions", s.withAuth(s.handleShellSessions))
	s.mux.HandleFunc("/operations", s.withAuth(s.handleOperations))
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := auth.Authorize(r, s.cfg.Token); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(api.StatusResponse{
		Version:        s.cfg.Version,
		Roots:          append([]string(nil), s.cfg.Roots...),
		Threshold:      s.cfg.LargeThreshold,
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		WatchSupported: true,
	})
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	op := s.startOperation(r, "manifest", root, map[string]any{"path": r.URL.Query().Get("path")})
	canonicalRoot, ok := s.canonicalRoot(root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	root = canonicalRoot
	op.Root = root
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}
	if path != "." {
		if _, err := paths.ResolveInsideWorkspace(root, path); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
			return
		}
	}
	resp, err := manifest.Scan(root, filepath.Clean(path), manifest.Options{LargeThreshold: s.cfg.LargeThreshold})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
	s.finishOperation(op, http.StatusOK, "success", "")
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	path := r.URL.Query().Get("path")
	op := s.startOperation(r, "download", root, map[string]any{"path": path, "range": r.Header.Get("Range")})
	canonicalRoot, ok := s.canonicalRoot(root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	root = canonicalRoot
	op.Root = root
	f, err := securefs.OpenExistingFile(root, path)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, paths.ErrPathEscape) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		s.finishOperation(op, status, "error", err.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		s.finishOperation(op, http.StatusInternalServerError, "error", err.Error())
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot download directory", http.StatusBadRequest)
		s.finishOperation(op, http.StatusBadRequest, "error", "cannot download directory")
		return
	}
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	http.ServeContent(recorder, r, info.Name(), info.ModTime(), f)
	s.finishOperation(op, recorder.status, statusResult(recorder.status), "")
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	op := s.startOperation(r, "apply", root, nil)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		s.finishOperation(op, http.StatusMethodNotAllowed, "error", "method not allowed")
		return
	}
	canonicalRoot, ok := s.canonicalRoot(root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	root = canonicalRoot
	op.Root = root
	r.Body = http.MaxBytesReader(w, r.Body, s.maxApplyBodyBytes())
	cs, err := s.decodeApplyChangeset(r.Context(), r.Body)
	if err != nil {
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		http.Error(w, err.Error(), status)
		s.finishOperation(op, status, "error", err.Error())
		return
	}
	op.RequestSummary = summarizeChanges(cs)
	op.ChangedPaths = changePaths(cs)
	result, err := apply.Apply(root, cs)
	if err != nil {
		status := http.StatusBadRequest
		resultName := "error"
		if errors.Is(err, apply.ErrConflict) {
			status = http.StatusConflict
			resultName = "conflict"
		} else if result.Error == "" {
			result.Error = err.Error()
		}
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(result)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(buf.Bytes())
		s.finishOperation(op, status, resultName, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
	s.finishOperation(op, http.StatusOK, "success", "")
}

func (s *Server) maxApplyBodyBytes() int64 {
	if s.cfg.MaxApplyBodyBytes > 0 {
		return s.cfg.MaxApplyBodyBytes
	}
	return limits.MaxApplyBodyBytes
}

func (s *Server) decodeApplyChangeset(parent context.Context, body io.ReadCloser) (apply.Changeset, error) {
	timeout := s.cfg.ApplyBodyReadTimeout
	if timeout <= 0 {
		timeout = limits.DefaultApplyBodyReadTimeout
	}
	var cs apply.Changeset
	err := decodeJSONBody(parent, body, timeout, &cs)
	return cs, err
}

func (s *Server) maxExecBodyBytes() int64 {
	if s.cfg.MaxExecBodyBytes > 0 {
		return s.cfg.MaxExecBodyBytes
	}
	return limits.MaxExecBodyBytes
}

func (s *Server) decodeExecRequest(parent context.Context, body io.ReadCloser) (api.ExecRequest, error) {
	timeout := s.cfg.ExecBodyReadTimeout
	if timeout <= 0 {
		timeout = limits.DefaultExecBodyReadTimeout
	}
	var req api.ExecRequest
	err := decodeJSONBody(parent, body, timeout, &req)
	return req, err
}

func decodeJSONBody(parent context.Context, body io.ReadCloser, timeout time.Duration, target any) error {
	idleBody := newIdleTimeoutReadCloser(parent, body, timeout)
	defer idleBody.Close()
	return json.NewDecoder(idleBody).Decode(target)
}

type idleTimeoutReadCloser struct {
	body      io.ReadCloser
	timeout   time.Duration
	done      chan struct{}
	closeOnce sync.Once
	bodyOnce  sync.Once
	mu        sync.Mutex
	err       error
}

func newIdleTimeoutReadCloser(ctx context.Context, body io.ReadCloser, timeout time.Duration) *idleTimeoutReadCloser {
	if ctx == nil {
		ctx = context.Background()
	}
	r := &idleTimeoutReadCloser{
		body:    body,
		timeout: timeout,
		done:    make(chan struct{}),
	}
	go func() {
		select {
		case <-ctx.Done():
			r.setErr(ctx.Err())
			_ = r.closeBody()
		case <-r.done:
		}
	}()
	return r
}

func (r *idleTimeoutReadCloser) Read(p []byte) (int, error) {
	if err := r.currentErr(); err != nil {
		return 0, err
	}
	timer := time.AfterFunc(r.timeout, func() {
		r.setErr(context.DeadlineExceeded)
		_ = r.closeBody()
	})
	n, err := r.body.Read(p)
	timer.Stop()
	if err != nil {
		if storedErr := r.currentErr(); storedErr != nil {
			return n, storedErr
		}
	}
	return n, err
}

func (r *idleTimeoutReadCloser) Close() error {
	r.closeOnce.Do(func() {
		close(r.done)
	})
	return r.closeBody()
}

func (r *idleTimeoutReadCloser) closeBody() error {
	var err error
	r.bodyOnce.Do(func() {
		err = r.body.Close()
	})
	return err
}

func (r *idleTimeoutReadCloser) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

func (r *idleTimeoutReadCloser) currentErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	op := s.startOperation(r, "exec", "", nil)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		s.finishOperation(op, http.StatusMethodNotAllowed, "error", "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxExecBodyBytes())
	req, err := s.decodeExecRequest(r.Context(), r.Body)
	if err != nil {
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		http.Error(w, err.Error(), status)
		s.finishOperation(op, status, "error", err.Error())
		return
	}
	rawRoot := req.Root
	op.Command = append([]string(nil), req.Command...)
	op.RequestSummary = map[string]any{"cwd": req.Cwd, "timeout_millis": req.TimeoutMillis}
	canonicalRoot, ok := s.canonicalRoot(req.Root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	req.Root = canonicalRoot
	op.Root = req.Root
	cwd := req.Root
	if req.Cwd != "" && req.Cwd != rawRoot && req.Cwd != req.Root {
		rel, ok := relativeToRoot(req.Cwd, rawRoot)
		if !ok {
			rel, ok = relativeToRoot(req.Cwd, req.Root)
		}
		if !ok {
			http.Error(w, "cwd is outside root", http.StatusBadRequest)
			s.finishOperation(op, http.StatusBadRequest, "error", "cwd is outside root")
			return
		}
		resolved, err := paths.ResolveExistingInsideWorkspace(req.Root, rel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
			return
		}
		cwd = resolved
	}
	timeout := time.Duration(req.TimeoutMillis) * time.Millisecond
	result, runErr := execx.Run(execx.Options{
		Context:        r.Context(),
		Cwd:            cwd,
		Command:        req.Command,
		Env:            req.Env,
		Timeout:        timeout,
		MaxOutputBytes: limits.MaxExecOutputBytes,
	})
	if runErr != nil && !result.TimedOut && result.ExitCode == 0 {
		result.ExitCode = 1
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
	op.ExitCode = result.ExitCode
	op.TimedOut = result.TimedOut
	resultName := "success"
	if result.TimedOut {
		resultName = "timeout"
	} else if result.ExitCode != 0 || runErr != nil {
		resultName = "error"
	}
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	s.finishOperation(op, http.StatusOK, resultName, errorMessage)
}

var wsUpgrader = websocket.Upgrader{CheckOrigin: allowEmptyOrSameOrigin}

func allowEmptyOrSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	op := s.startOperation(r, "events", root, nil)
	canonicalRoot, ok := s.canonicalRoot(root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	root = canonicalRoot
	op.Root = root
	watcher, err := watch.New(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		s.finishOperation(op, http.StatusInternalServerError, "error", err.Error())
		return
	}
	defer watcher.Close()
	if err := watcher.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		s.finishOperation(op, http.StatusInternalServerError, "error", err.Error())
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
		return
	}
	defer conn.Close()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()
	defer s.finishOperation(op, http.StatusOK, "success", "")
	for {
		select {
		case ev := <-watcher.Events():
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		case <-clientDone:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	sessionID := r.URL.Query().Get("session")
	shellCommand := interactiveShellCommand()
	op := s.startOperation(r, "shell", root, map[string]any{"shell": strings.Join(shellCommand, " "), "rows": 24, "cols": 80, "session": sessionID})
	canonicalRoot, ok := s.canonicalRoot(root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	root = canonicalRoot
	op.Root = root
	var session *ptysession.Session
	var sub *ptysession.OutputSubscription
	var err error
	if sessionID != "" {
		session = s.ptyManager.Get(sessionID)
		if session == nil || session.Root != root {
			http.Error(w, "shell session not found", http.StatusNotFound)
			s.finishOperation(op, http.StatusNotFound, "error", "shell session not found")
			return
		}
		var ok bool
		sub, ok = session.Attach(250 * time.Millisecond)
		if !ok {
			http.Error(w, "shell session already attached", http.StatusConflict)
			s.finishOperation(op, http.StatusConflict, "error", "shell session already attached")
			return
		}
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		if sub != nil {
			sub.Detach()
		}
		s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
		return
	}
	defer conn.Close()
	if sessionID == "" {
		session, err = s.ptyManager.Start(ptysession.StartOptions{Command: shellCommand, Cwd: root, Root: root, Rows: 24, Cols: 80})
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
			s.finishOperation(op, http.StatusInternalServerError, "error", err.Error())
			return
		}
		var ok bool
		sub, ok = session.Attach(0)
		if !ok {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("shell session already attached"))
			s.finishOperation(op, http.StatusConflict, "error", "shell session already attached")
			return
		}
	}
	defer sub.Detach()
	op.RequestSummary["session"] = session.ID
	statusCode := http.StatusOK
	resultName := "success"
	errorMessage := ""
	applyShellStatus := func(status ptysession.ExitStatus) {
		op.ExitCode = status.ExitCode
		if status.ExitCode != 0 {
			resultName = "error"
			if status.Err != nil {
				errorMessage = status.Err.Error()
			}
		}
	}
	defer func() {
		s.finishOperation(op, statusCode, resultName, errorMessage)
	}()

	done := make(chan ptysession.ExitStatus, 1)
	go func() {
		defer conn.Close()
		for {
			select {
			case frame := <-sub.Frames:
				if len(frame.Data) > 0 {
					if err := conn.WriteMessage(websocket.BinaryMessage, frame.Data); err != nil {
						return
					}
				}
				if frame.ExitStatus != nil {
					_ = conn.WriteJSON(api.ShellFrame{Type: "exit", ExitCode: frame.ExitStatus.ExitCode})
					done <- *frame.ExitStatus
					return
				}
			case <-sub.Done():
				return
			case <-r.Context().Done():
				return
			}
		}
	}()
	for {
		select {
		case status := <-done:
			applyShellStatus(status)
			return
		default:
		}
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			select {
			case status := <-done:
				applyShellStatus(status)
			default:
			}
			return
		}
		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
			if handled, err := handleShellFrame(session, msg); handled || err != nil {
				if err != nil {
					return
				}
				continue
			}
			if _, err := session.Write(msg); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleShellSessions(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	canonicalRoot, ok := s.canonicalRoot(root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		sessions := make([]api.ShellSessionInfo, 0)
		listed := s.ptyManager.List()
		for i := range listed {
			session := &listed[i]
			if session.Root != canonicalRoot {
				continue
			}
			sessions = append(sessions, api.ShellSessionInfo{
				ID:         session.ID,
				Command:    append([]string(nil), session.Command...),
				LastActive: session.LastActive.UTC().Format(time.RFC3339Nano),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}
		session := s.ptyManager.Get(id)
		if session == nil || session.Root != canonicalRoot {
			http.Error(w, "shell session not found", http.StatusNotFound)
			return
		}
		_ = s.ptyManager.Close(id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleShellFrame(session *ptysession.Session, msg []byte) (bool, error) {
	var frame api.ShellFrame
	if err := json.Unmarshal(msg, &frame); err != nil || frame.Type == "" {
		return false, nil
	}
	switch frame.Type {
	case "resize":
		return true, session.Resize(frame.Rows, frame.Cols)
	case "data":
		if len(frame.Data) == 0 {
			return true, nil
		}
		_, err := session.Write(frame.Data)
		return true, err
	default:
		return true, nil
	}
}

func interactiveShellCommand() []string {
	if shell := firstExecutableShell(os.Getenv("SHELL")); shell != "" {
		return []string{shell, "-i"}
	}
	for _, candidate := range []string{"/bin/bash", "/usr/bin/bash", "/bin/zsh", "/usr/bin/zsh", "/bin/sh", "/usr/bin/sh"} {
		if shell := firstExecutableShell(candidate); shell != "" {
			return []string{shell, "-i"}
		}
	}
	return []string{"sh"}
}

func firstExecutableShell(shell string) string {
	if shell == "" || !filepath.IsAbs(shell) {
		return ""
	}
	if info, err := os.Stat(shell); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return shell
	}
	if resolved, err := exec.LookPath(shell); err == nil {
		return resolved
	}
	return ""
}

func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		http.Error(w, "root is required", http.StatusBadRequest)
		return
	}
	canonicalRoot, ok := s.canonicalRoot(root)
	if !ok {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	store := s.operationStore(canonicalRoot)
	if store == nil {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	entries, err := store.List(ops.Filter{Root: canonicalRoot, Limit: limit})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
}

func (s *Server) allowedRoot(root string) bool {
	_, ok := s.canonicalRoot(root)
	return ok
}

func (s *Server) canonicalRoot(root string) (string, bool) {
	canonical, ok, err := remoteroot.ResolveAllowed(s.allowedRoots, root)
	if err != nil || !ok {
		return "", false
	}
	return canonical, true
}

func (s *Server) startOperation(r *http.Request, operation, root string, summary map[string]any) ops.Entry {
	id := ops.NewID()
	clientID := r.Header.Get(api.HeaderClientID)
	if clientID == "" {
		clientID = "unknown"
	}
	return ops.Entry{
		ID:             id,
		StartedAt:      time.Now().UTC(),
		ClientID:       clientID,
		Root:           root,
		Operation:      operation,
		RequestSummary: summary,
	}
}

func (s *Server) finishOperation(entry ops.Entry, statusCode int, result string, errMsg string) {
	entry.FinishedAt = time.Now().UTC()
	entry.StatusCode = statusCode
	entry.Result = result
	entry.ErrorMessage = errMsg
	if canonical, ok := s.canonicalRoot(entry.Root); ok {
		entry.Root = canonical
	}
	store := s.operationStore(entry.Root)
	if store == nil {
		return
	}
	_ = store.Append(entry)
}

func (s *Server) operationStore(root string) ops.Store {
	cleanRoot, ok := s.canonicalRoot(root)
	if !ok {
		return nil
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	store := s.operationStores[cleanRoot]
	if store == nil {
		store = ops.NewJSONLStore(operationLogPath(cleanRoot))
		s.operationStores[cleanRoot] = store
	}
	return store
}

func relativeToRoot(candidate, root string) (string, bool) {
	cleanRoot := filepath.Clean(root)
	cleanCandidate := filepath.Clean(candidate)
	rel, err := filepath.Rel(cleanRoot, cleanCandidate)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func summarizeChanges(cs apply.Changeset) map[string]any {
	changes := make([]map[string]any, 0, len(cs.Changes))
	for _, ch := range cs.Changes {
		item := map[string]any{
			"path":          ch.Path,
			"kind":          ch.Kind,
			"content_bytes": len(ch.Content),
		}
		if ch.BaseHash != "" {
			item["base_hash_prefix"] = hashPrefix(ch.BaseHash)
		}
		changes = append(changes, item)
	}
	return map[string]any{"changes": changes}
}

func changePaths(cs apply.Changeset) []string {
	paths := make([]string, 0, len(cs.Changes))
	for _, ch := range cs.Changes {
		paths = append(paths, ch.Path)
	}
	return paths
}

func hashPrefix(hash string) string {
	if len(hash) <= 19 {
		return hash
	}
	return hash[:19]
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func statusResult(status int) string {
	if status >= 200 && status < 300 {
		return "success"
	}
	if status == http.StatusConflict {
		return "conflict"
	}
	return "error"
}

func operationLogPath(root string) string {
	return filepath.Join(root, ".remork", "log", "operations.jsonl")
}

func normalizeConfiguredRoots(roots []string) ([]string, error) {
	normalized := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			return nil, fmt.Errorf("root is required")
		}
		if !filepath.IsAbs(root) {
			abs, err := filepath.Abs(root)
			if err != nil {
				return nil, err
			}
			root = abs
		}
		normalized = append(normalized, filepath.Clean(root))
	}
	return normalized, nil
}
