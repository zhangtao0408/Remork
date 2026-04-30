package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"remork/internal/api"
	"remork/internal/apply"
	execx "remork/internal/exec"
	"remork/internal/manifest"
	"remork/internal/paths"
	"remork/internal/watch"
)

type Config struct {
	Roots          []string
	LargeThreshold int64
}

type Server struct {
	cfg Config
	mux *http.ServeMux
}

func NewServer(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("/manifest", s.handleManifest)
	s.mux.HandleFunc("/download", s.handleDownload)
	s.mux.HandleFunc("/apply", s.handleApply)
	s.mux.HandleFunc("/exec", s.handleExec)
	s.mux.HandleFunc("/events", s.handleEvents)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}
	if path != "." {
		if _, err := paths.ResolveInsideWorkspace(root, path); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	resp, err := manifest.Scan(root, filepath.Clean(path), manifest.Options{LargeThreshold: s.cfg.LargeThreshold})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	path := r.URL.Query().Get("path")
	full, err := paths.ResolveExistingInsideWorkspace(root, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot download directory", http.StatusBadRequest)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	var cs apply.Changeset
	if err := json.NewDecoder(r.Body).Decode(&cs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := apply.Apply(root, cs)
	if err != nil {
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(result)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write(buf.Bytes())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req api.ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.allowedRoot(req.Root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	cwd := req.Root
	if req.Cwd != "" && req.Cwd != req.Root {
		rel := strings.TrimPrefix(req.Cwd, req.Root+string(os.PathSeparator))
		resolved, err := paths.ResolveInsideWorkspace(req.Root, rel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
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
}

var wsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if !s.allowedRoot(root) {
		http.Error(w, "root not allowed", http.StatusForbidden)
		return
	}
	watcher, err := watch.New(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer watcher.Close()
	if err := watcher.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
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

func (s *Server) allowedRoot(root string) bool {
	for _, r := range s.cfg.Roots {
		if r == root {
			return true
		}
	}
	return false
}
