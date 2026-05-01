package ptysession

import (
	"os"
	"strings"
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

func TestSessionOutputAfterReattachGoesToCurrentSubscriber(t *testing.T) {
	skipPTYIfRequested(t)
	m := NewManager(time.Second)
	s, err := m.Start(StartOptions{Command: []string{"sh"}, Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.CloseSession(s)

	first, ok := s.Attach(0)
	if !ok {
		t.Fatal("first attach failed")
	}
	if _, err := s.Write([]byte("printf 'ready\\n'\n")); err != nil {
		t.Fatalf("write ready: %v", err)
	}
	readOutputContaining(t, first, "ready")
	first.Detach()

	second, ok := s.Attach(0)
	if !ok {
		t.Fatal("second attach failed")
	}
	defer second.Detach()
	first.Detach()

	if _, err := s.Write([]byte("printf 'attached\\n'; exit\n")); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	readOutputContaining(t, second, "attached")
}

func TestSessionPublishBuffersWhenSubscriberTokenIsStale(t *testing.T) {
	s := &Session{LastActive: time.Now()}
	first, ok := s.Attach(0)
	if !ok {
		t.Fatal("first attach failed")
	}

	s.attachMu.Lock()
	s.attachID++
	s.attached = false
	s.attachMu.Unlock()

	s.publish(OutputFrame{Data: []byte("after-detach")})

	select {
	case frame := <-first.Frames:
		t.Fatalf("stale subscriber received frame %q", string(frame.Data))
	default:
	}

	second, ok := s.Attach(0)
	if !ok {
		t.Fatal("second attach failed")
	}
	readOutputContaining(t, second, "after-detach")
}

func readOutputContaining(t *testing.T, sub *OutputSubscription, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	var transcript strings.Builder
	for !strings.Contains(transcript.String(), want) {
		select {
		case frame := <-sub.Frames:
			if frame.ExitStatus != nil {
				t.Fatalf("session exited before %q; transcript:\n%s", want, transcript.String())
			}
			transcript.Write(frame.Data)
		case <-deadline:
			t.Fatalf("timed out waiting for %q; transcript:\n%s", want, transcript.String())
		}
	}
}

func skipPTYIfRequested(t *testing.T) {
	t.Helper()
	if os.Getenv("REMORK_SKIP_PTY_TESTS") == "1" {
		t.Skip("PTY tests disabled by REMORK_SKIP_PTY_TESTS")
	}
}
