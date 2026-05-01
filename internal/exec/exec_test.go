package execx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesStdoutAndExitCode(t *testing.T) {
	res, err := Run(Options{Command: []string{"sh", "-c", "echo hello"}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 || res.Stdout != "hello\n" {
		t.Fatalf("bad result: %#v", res)
	}
}

func TestRunTimeoutKillsCommand(t *testing.T) {
	res, err := Run(Options{Command: []string{"sh", "-c", "sleep 2"}, Timeout: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !res.TimedOut {
		t.Fatalf("expected timed out result: %#v", res)
	}
}

func TestRunEmptyCommandFails(t *testing.T) {
	res, err := Run(Options{})
	if err == nil {
		t.Fatal("expected empty command error")
	}
	if res.ExitCode != -1 {
		t.Fatalf("exit code %d", res.ExitCode)
	}
}

func TestRunTruncatesLargeStdout(t *testing.T) {
	res, err := Run(Options{
		Command:        []string{"sh", "-c", "yes x | head -c 9000000"},
		MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.StdoutTruncated {
		t.Fatalf("stdout was not marked truncated")
	}
	if len(res.Stdout) > 1200 {
		t.Fatalf("stdout too large: %d", len(res.Stdout))
	}
}

func TestRunParentContextCancellationReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	resCh := make(chan Result, 1)

	go func() {
		res, err := Run(Options{
			Context: ctx,
			Command: []string{"sh", "-c", "sleep 2"},
			Timeout: time.Minute,
		})
		resCh <- res
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		res := <-resCh
		if err == nil {
			t.Fatal("expected cancellation error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if res.TimedOut {
			t.Fatalf("parent cancellation should not be marked timeout: %#v", res)
		}
		if res.ExitCode == 0 {
			t.Fatalf("exit code = %d, want non-success for canceled command", res.ExitCode)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("run did not return promptly after parent context cancellation")
	}
}

func TestRunTimeoutKillsProcessGroupWithPipeHoldingChild(t *testing.T) {
	if !isUnixForProcessGroupTest() {
		t.Skip("process group cancellation test is Unix-specific")
	}
	start := time.Now()
	res, err := Run(Options{
		Command: []string{"sh", "-c", "sleep 2 & wait"},
		Timeout: 20 * time.Millisecond,
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !res.TimedOut {
		t.Fatalf("expected timed out result: %#v", res)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("run returned after %s, want prompt process-group cancellation", elapsed)
	}
}

func TestRunCleansProcessGroupAfterMainProcessExits(t *testing.T) {
	if !isUnixForProcessGroupTest() {
		t.Skip("process group cleanup test is Unix-specific")
	}
	marker := filepath.Join(t.TempDir(), "child-survived")
	start := time.Now()
	res, err := Run(Options{
		Command: []string{"sh", "-c", "(sleep 0.2; touch " + shellQuote(marker) + ") & echo done"},
		Timeout: time.Second,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run: %v; result: %#v", err, res)
	}
	if res.ExitCode != 0 || res.Stdout != "done\n" {
		t.Fatalf("result = %#v, want successful parent output", res)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("run returned after %s, want prompt cleanup after parent exit", elapsed)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("background child survived process-group cleanup and created marker")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat marker: %v", err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func isUnixForProcessGroupTest() bool {
	switch runtime.GOOS {
	case "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "linux", "netbsd", "openbsd", "solaris":
		return true
	default:
		return false
	}
}
