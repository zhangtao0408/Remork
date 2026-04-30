package ops

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

func NewJSONLStore(path string) *JSONLStore {
	return &JSONLStore{path: path}
}

func (s *JSONLStore) Append(entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
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
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []Entry
	scanner := bufio.NewScanner(f)
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
