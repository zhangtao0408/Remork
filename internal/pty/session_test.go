package ptysession

import (
	"os"
	"testing"
	"time"
)

func TestManagerStartsListsAndClosesSession(t *testing.T) {
	skipPTYIfRequested(t)
	m := NewManager(100 * time.Millisecond)
	s, err := m.Start(StartOptions{Command: []string{"sh"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(m.List()) != 1 {
		t.Fatal("missing session")
	}
	if err := m.Close(s.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("session not removed")
	}
}

func TestManagerReapsIdleSession(t *testing.T) {
	skipPTYIfRequested(t)
	m := NewManager(10 * time.Millisecond)
	_, err := m.Start(StartOptions{Command: []string{"sh"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	m.ReapIdle()
	if len(m.List()) != 0 {
		t.Fatal("idle session not reaped")
	}
}

func TestCloseUnknownSessionIsNoop(t *testing.T) {
	m := NewManager(time.Second)
	if err := m.Close("missing"); err != nil {
		t.Fatalf("close missing: %v", err)
	}
}

func TestSessionWaitReturnsExitCode(t *testing.T) {
	skipPTYIfRequested(t)
	m := NewManager(time.Second)
	s, err := m.Start(StartOptions{Command: []string{"sh", "-c", "exit 7"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.CloseSession(s)
	status := s.Wait()
	if status.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7; err=%v", status.ExitCode, status.Err)
	}
}

func skipPTYIfRequested(t *testing.T) {
	t.Helper()
	if os.Getenv("REMORK_SKIP_PTY_TESTS") == "1" {
		t.Skip("PTY tests disabled by REMORK_SKIP_PTY_TESTS")
	}
}
