package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"remork/internal/apply"
	"remork/internal/manifest"
	"remork/internal/paths"
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

func (s *Server) allowedRoot(root string) bool {
	for _, r := range s.cfg.Roots {
		if r == root {
			return true
		}
	}
	return false
}
