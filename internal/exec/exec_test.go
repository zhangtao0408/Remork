package execx

import (
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
