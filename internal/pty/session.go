package ptysession

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

type StartOptions struct {
	Command []string
	Cwd     string
	Env     []string
	Rows    uint16
	Cols    uint16
}

type Session struct {
	ID         string
	Command    []string
	LastActive time.Time
	mu         sync.Mutex
	cmd        *exec.Cmd
	file       *os.File
}

func (s *Session) Read(p []byte) (int, error) {
	s.touch()
	return s.file.Read(p)
}

func (s *Session) Write(p []byte) (int, error) {
	s.touch()
	return s.file.Write(p)
}

func (s *Session) snapshot() Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Session{ID: s.ID, Command: append([]string(nil), s.Command...), LastActive: s.LastActive}
}

func (s *Session) touch() {
	s.mu.Lock()
	s.LastActive = time.Now()
	s.mu.Unlock()
}

type Manager struct {
	mu        sync.Mutex
	retention time.Duration
	sessions  map[string]*Session
}

func NewManager(retention time.Duration) *Manager {
	return &Manager{retention: retention, sessions: map[string]*Session{}}
}

func (m *Manager) Start(opts StartOptions) (*Session, error) {
	if len(opts.Command) == 0 {
		opts.Command = []string{"sh"}
	}
	cmd := exec.Command(opts.Command[0], opts.Command[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = append(os.Environ(), opts.Env...)
	rows, cols := opts.Rows, opts.Cols
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	s := &Session{ID: randomID(), Command: opts.Command, LastActive: time.Now(), cmd: cmd, file: f}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.snapshot())
	}
	return out
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	_ = s.file.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

func (m *Manager) CloseSession(s *Session) error {
	if s == nil {
		return nil
	}
	return m.Close(s.ID)
}

func (m *Manager) ReapIdle() {
	for _, s := range m.List() {
		if time.Since(s.LastActive) > m.retention {
			_ = m.Close(s.ID)
		}
	}
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
