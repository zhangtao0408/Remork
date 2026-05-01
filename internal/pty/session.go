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
	Root    string
	Env     []string
	Rows    uint16
	Cols    uint16
}

type Session struct {
	ID            string
	Command       []string
	Root          string
	LastActive    time.Time
	mu            sync.RWMutex
	attachMu      sync.Mutex
	attached      bool
	attachID      uint64
	subscriber    *OutputSubscription
	outputBacklog []OutputFrame
	cmd           *exec.Cmd
	file          *os.File
	waitDone      chan struct{}
	exitStatus    ExitStatus
}

type ExitStatus struct {
	ExitCode int
	Err      error
}

type OutputFrame struct {
	Data       []byte
	ExitStatus *ExitStatus
}

type OutputSubscription struct {
	Frames  <-chan OutputFrame
	session *Session
	id      uint64
	frames  chan OutputFrame
	done    chan struct{}
}

func (s *Session) Write(p []byte) (int, error) {
	s.touch()
	return s.file.Write(p)
}

func (s *Session) Resize(rows, cols int) error {
	if rows <= 0 || cols <= 0 {
		return nil
	}
	s.touch()
	return pty.Setsize(s.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (s *Session) Wait() ExitStatus {
	<-s.waitDone
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exitStatus
}

func (s *Session) Attach(timeout time.Duration) (*OutputSubscription, bool) {
	deadline := time.Now().Add(timeout)
	for {
		s.attachMu.Lock()
		if !s.attached {
			sub := s.newSubscriptionLocked()
			s.attachMu.Unlock()
			s.touch()
			return sub, true
		}
		s.attachMu.Unlock()
		if timeout <= 0 || time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Session) newSubscriptionLocked() *OutputSubscription {
	s.attachID++
	sub := &OutputSubscription{
		session: s,
		id:      s.attachID,
		frames:  make(chan OutputFrame, outputBufferLimit),
		done:    make(chan struct{}),
	}
	sub.Frames = sub.frames
	for _, frame := range s.outputBacklog {
		sub.frames <- frame
	}
	s.outputBacklog = nil
	s.subscriber = sub
	s.attached = true
	return sub
}

func (sub *OutputSubscription) Detach() {
	if sub == nil || sub.session == nil {
		return
	}
	sub.session.detach(sub)
}

func (sub *OutputSubscription) Done() <-chan struct{} {
	if sub == nil {
		return nil
	}
	return sub.done
}

func (s *Session) detach(sub *OutputSubscription) {
	s.attachMu.Lock()
	if s.subscriber != sub || s.attachID != sub.id {
		s.attachMu.Unlock()
		return
	}
	close(sub.done)
	s.subscriber = nil
	s.attached = false
	s.attachMu.Unlock()
	s.touch()
}

func (s *Session) snapshot() Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Session{ID: s.ID, Command: append([]string(nil), s.Command...), Root: s.Root, LastActive: s.LastActive}
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

const outputBufferLimit = 64

func NewManager(retention time.Duration) *Manager {
	return &Manager{retention: retention, sessions: map[string]*Session{}}
}

func (m *Manager) Start(opts StartOptions) (*Session, error) {
	m.reapIdle()
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
	root := opts.Root
	if root == "" {
		root = opts.Cwd
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	s := &Session{ID: randomID(), Command: append([]string(nil), opts.Command...), Root: root, LastActive: time.Now(), cmd: cmd, file: f, waitDone: make(chan struct{})}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	go s.readOutput()
	go func() {
		s.wait()
		m.remove(s.ID)
		_ = s.file.Close()
	}()
	return s, nil
}

func (s *Session) readOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := s.file.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			s.publish(OutputFrame{Data: data})
		}
		if err != nil {
			status := s.Wait()
			s.publish(OutputFrame{ExitStatus: &status})
			return
		}
	}
}

func (s *Session) publish(frame OutputFrame) {
	s.touch()
	s.attachMu.Lock()
	sub := s.subscriber
	if sub == nil || s.attachID != sub.id {
		s.bufferOutputLocked(frame)
		s.attachMu.Unlock()
		return
	}

	select {
	case sub.frames <- frame:
	default:
		select {
		case <-sub.frames:
		default:
		}
		select {
		case sub.frames <- frame:
		default:
		}
	}
	s.attachMu.Unlock()
}

func (s *Session) bufferOutputLocked(frame OutputFrame) {
	if frame.ExitStatus != nil {
		return
	}
	s.outputBacklog = append(s.outputBacklog, frame)
	if len(s.outputBacklog) > outputBufferLimit {
		copy(s.outputBacklog, s.outputBacklog[len(s.outputBacklog)-outputBufferLimit:])
		s.outputBacklog = s.outputBacklog[:outputBufferLimit]
	}
}

func (s *Session) wait() {
	err := s.cmd.Wait()
	status := ExitStatus{Err: err}
	if s.cmd.ProcessState != nil {
		status.ExitCode = s.cmd.ProcessState.ExitCode()
	} else if err != nil {
		status.ExitCode = 1
	}
	s.mu.Lock()
	s.exitStatus = status
	s.mu.Unlock()
	close(s.waitDone)
}

func (m *Manager) Get(id string) *Session {
	m.reapIdle()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) List() []Session {
	m.reapIdle()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.snapshot())
	}
	return out
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	closeSession(s)
	return nil
}

func (m *Manager) CloseSession(s *Session) error {
	if s == nil {
		return nil
	}
	return m.Close(s.ID)
}

func (m *Manager) ReapIdle() {
	m.reapIdle()
}

func (m *Manager) reapIdle() {
	if m.retention <= 0 {
		return
	}
	now := time.Now()
	var expired []*Session
	m.mu.Lock()
	for id, s := range m.sessions {
		s.mu.RLock()
		idle := now.Sub(s.LastActive)
		s.mu.RUnlock()
		if idle > m.retention {
			delete(m.sessions, id)
			expired = append(expired, s)
		}
	}
	m.mu.Unlock()
	for _, s := range expired {
		closeSession(s)
	}
}

func closeSession(s *Session) {
	if s == nil {
		return
	}
	_ = s.file.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
