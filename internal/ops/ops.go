package ops

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	ID             string         `json:"op_id"`
	StartedAt      time.Time      `json:"time_start"`
	FinishedAt     time.Time      `json:"time_end"`
	ClientID       string         `json:"client_id"`
	Root           string         `json:"workspace_root"`
	Operation      string         `json:"operation"`
	RequestSummary map[string]any `json:"request_summary,omitempty"`
	Result         string         `json:"result"`
	StatusCode     int            `json:"status_code"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	ChangedPaths   []string       `json:"changed_paths,omitempty"`
	Command        []string       `json:"command,omitempty"`
	ExitCode       int            `json:"exit_code,omitempty"`
	TimedOut       bool           `json:"timed_out,omitempty"`
}

type Filter struct {
	Root  string
	Since time.Time
	Limit int
}

type Store interface {
	Append(Entry) error
	List(Filter) ([]Entry, error)
}

type MemoryStore struct {
	mu      sync.Mutex
	entries []Entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}

func (s *MemoryStore) List(filter Filter) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return applyFilter(append([]Entry(nil), s.entries...), filter), nil
}

type JSONLStore struct {
	mu   sync.Mutex
	path string
}

const maxJSONLLineBytes = 16 << 20

func NewJSONLStore(path string) *JSONLStore {
	return &JSONLStore{path: normalizeLogPath(path)}
}

func (s *JSONLStore) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ensureSafeLogPath(s.path); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if err := verifyOpenFileIsNotSymlink(s.path, f); err != nil {
		_ = f.Close()
		return err
	}
	defer f.Close()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *JSONLStore) List(filter Filter) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := rejectSymlinkParents(filepath.Dir(s.path)); err != nil {
		return nil, err
	}
	if err := rejectSymlinkFinal(s.path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := verifyOpenFileIsNotSymlink(s.path, f); err != nil {
		_ = f.Close()
		return nil, err
	}
	defer f.Close()
	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), maxJSONLLineBytes)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return applyFilter(entries, filter), nil
}

func ensureSafeLogPath(logPath string) error {
	if err := mkdirAllNoSymlink(filepath.Dir(logPath)); err != nil {
		return err
	}
	if err := rejectSymlinkFinal(logPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func normalizeLogPath(logPath string) string {
	abs, err := filepath.Abs(logPath)
	if err != nil {
		abs = filepath.Clean(logPath)
	}
	base := filepath.Base(abs)
	dir := filepath.Dir(abs)
	var suffix []string
	for {
		if filepath.Base(dir) == ".remork" {
			prefix := filepath.Dir(dir)
			if evaluated, err := filepath.EvalSymlinks(prefix); err == nil {
				parts := append([]string{evaluated, ".remork"}, reverseStrings(suffix)...)
				parts = append(parts, base)
				return filepath.Join(parts...)
			}
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		suffix = append(suffix, filepath.Base(dir))
		dir = parent
	}
	if evaluated, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(evaluated, base)
	}
	return abs
}

func reverseStrings(in []string) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}

func mkdirAllNoSymlink(dir string) error {
	clean := filepath.Clean(dir)
	parent := filepath.Dir(clean)
	if parent != clean {
		if err := mkdirAllNoSymlink(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(clean)
	if os.IsNotExist(err) {
		if err := os.Mkdir(clean, 0o755); err != nil {
			if os.IsExist(err) {
				return mkdirAllNoSymlink(clean)
			}
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("operation log directory %q is a symlink", clean)
	}
	if !info.IsDir() {
		return fmt.Errorf("operation log path %q is not a directory", clean)
	}
	return nil
}

func rejectSymlinkParents(dir string) error {
	clean := filepath.Clean(dir)
	parent := filepath.Dir(clean)
	if parent != clean {
		if err := rejectSymlinkParents(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(clean)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("operation log directory %q is a symlink", clean)
	}
	if !info.IsDir() {
		return fmt.Errorf("operation log path %q is not a directory", clean)
	}
	return nil
}

func rejectSymlinkFinal(logPath string) error {
	info, err := os.Lstat(logPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("operation log path %q is a symlink", logPath)
	}
	if info.IsDir() {
		return fmt.Errorf("operation log path %q is a directory", logPath)
	}
	return nil
}

func verifyOpenFileIsNotSymlink(path string, f *os.File) error {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("operation log path %q is a symlink", path)
	}
	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(pathInfo, fileInfo) {
		return fmt.Errorf("operation log path %q changed while opening", path)
	}
	return nil
}

func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "op-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "op-" + hex.EncodeToString(b[:])
}

func applyFilter(entries []Entry, filter Filter) []Entry {
	var out []Entry
	for _, entry := range entries {
		if filter.Root != "" && entry.Root != filter.Root {
			continue
		}
		if !filter.Since.IsZero() && !entry.StartedAt.After(filter.Since) {
			continue
		}
		out = append(out, entry)
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[len(out)-filter.Limit:]
	}
	return out
}
