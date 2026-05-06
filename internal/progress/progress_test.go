package progress

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTextReporterShowsProgressWhenInteractive(t *testing.T) {
	var buf lockedBuffer
	r := NewTextReporter(&buf, Options{Quiet: false})
	r.Start("download", 100)
	r.Advance(40)
	r.Advance(60)
	r.Done()
	out := buf.String()
	if !strings.Contains(out, "download") || !strings.Contains(out, "100/100") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "[") {
		t.Fatalf("progress output should contain a bar, got: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("progress should rewrite one terminal line and only finish with one newline, got: %q", out)
	}
	if !strings.Contains(out, "\r") {
		t.Fatalf("progress should use carriage returns to rewrite the current line, got: %q", out)
	}
}

func TestTextReporterRewritesRunningStepToOk(t *testing.T) {
	var buf lockedBuffer
	r := NewTextReporter(&buf, Options{Quiet: false})
	r.Start("sync: fetching remote manifest", 1)
	r.Done()

	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Fatalf("step should be rewritten in place, got %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("step should only finish with one newline, got %q", out)
	}
	if strings.Contains(out, "-> sync: fetching remote manifest\n") {
		t.Fatalf("step should not leave a separate running line before ok, got %q", out)
	}
	if !strings.Contains(out, "ok sync: fetching remote manifest") {
		t.Fatalf("step should finish with ok on the rewritten line, got %q", out)
	}
}

func TestTextReporterUsesRemorkSpinnerFrames(t *testing.T) {
	var buf lockedBuffer
	r := NewTextReporter(&buf, Options{Quiet: false})
	r.Start("sync: fetching remote manifest", 1)
	waitForOutput(t, &buf, "o sync: fetching remote manifest")
	r.Done()

	out := buf.String()
	for _, want := range []string{". sync: fetching remote manifest", "o sync: fetching remote manifest"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain spinner frame %q, got %q", want, out)
		}
	}
}

func waitForOutput(t *testing.T, buf *lockedBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", want, buf.String())
}

func TestTextReporterQuietSuppressesOutput(t *testing.T) {
	var buf lockedBuffer
	r := NewTextReporter(&buf, Options{Quiet: true})
	r.Start("download", 100)
	r.Advance(100)
	r.Done()
	if buf.String() != "" {
		t.Fatalf("quiet output: %q", buf.String())
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
