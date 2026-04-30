package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"remork/internal/api"
	"remork/internal/apply"
	execx "remork/internal/exec"
	"remork/internal/manifest"
	"remork/internal/ops"
	"remork/internal/paths"
	ptysession "remork/internal/pty"
	"remork/internal/watch"
)

type Config struct {
	Roots            []string
	LargeThreshold   int64
	OperationLogPath string
}

type Server struct {
	cfg            Config
	mux            *http.ServeMux
	ptyManager     *ptysession.Manager
	operationStore ops.Store
}

func NewServer(cfg Config) *Server {
	store := ops.Store(ops.NewMemoryStore())
	if cfg.OperationLogPath != "" {
		store = ops.NewJSONLStore(cfg.OperationLogPath)
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux(), ptyManager: ptysession.NewManager(30 * time.Minute), operationStore: store}
	s.mux.HandleFunc("/manifest", s.handleManifest)
	s.mux.HandleFunc("/download", s.handleDownload)
	s.mux.HandleFunc("/apply", s.handleApply)
	s.mux.HandleFunc("/exec", s.handleExec)
	s.mux.HandleFunc("/events", s.handleEvents)
	s.mux.HandleFunc("/shell", s.handleShell)
	s.mux.HandleFunc("/operations", s.handleOperations)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	op := s.startOperation(r, "manifest", root, map[string]any{"path": r.URL.Query().Get("path")})
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
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
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	full, err := paths.ResolveExistingInsideWorkspace(root, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		s.finishOperation(op, http.StatusNotFound, "error", err.Error())
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
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	var cs apply.Changeset
	if err := json.NewDecoder(r.Body).Decode(&cs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
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

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	op := s.startOperation(r, "exec", "", nil)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		s.finishOperation(op, http.StatusMethodNotAllowed, "error", "method not allowed")
		return
	}
	var req api.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
		return
	}
	op.Root = req.Root
	op.Command = append([]string(nil), req.Command...)
	op.RequestSummary = map[string]any{"cwd": req.Cwd, "timeout_millis": req.TimeoutMillis}
	if !s.allowedRoot(req.Root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	cwd := req.Root
	if req.Cwd != "" && req.Cwd != req.Root {
		rel := strings.TrimPrefix(req.Cwd, req.Root+string(os.PathSeparator))
		resolved, err := paths.ResolveInsideWorkspace(req.Root, rel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
			return
		}
		cwd = resolved
	}
	timeout := time.Duration(req.TimeoutMillis) * time.Millisecond
	result, runErr := execx.Run(execx.Options{Cwd: cwd, Command: req.Command, Env: req.Env, Timeout: timeout})
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

var wsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	op := s.startOperation(r, "events", root, nil)
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
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
	defer s.finishOperation(op, http.StatusOK, "success", "")
	for {
		select {
		case ev := <-watcher.Events():
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	op := s.startOperation(r, "shell", root, map[string]any{"shell": "sh", "rows": 24, "cols": 80})
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		s.finishOperation(op, http.StatusForbidden, "error", "root not allowed")
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.finishOperation(op, http.StatusBadRequest, "error", err.Error())
		return
	}
	defer conn.Close()
	session, err := s.ptyManager.Start(ptysession.StartOptions{Command: []string{"sh"}, Cwd: root, Rows: 24, Cols: 80})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
		s.finishOperation(op, http.StatusInternalServerError, "error", err.Error())
		return
	}
	defer s.ptyManager.CloseSession(session)
	defer s.finishOperation(op, http.StatusOK, "success", "")

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := session.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
			if _, err := session.Write(msg); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root != "" && !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	entries, err := s.operationStore.List(ops.Filter{Root: root, Limit: limit})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
}

func (s *Server) allowedRoot(root string) bool {
	for _, r := range s.cfg.Roots {
		if r == root {
			return true
		}
	}
	return false
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
	_ = s.operationStore.Append(entry)
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
