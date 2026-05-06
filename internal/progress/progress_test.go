package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestTextReporterShowsProgressWhenInteractive(t *testing.T) {
	var buf bytes.Buffer
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
}

func TestTextReporterUsesStructuredPlainOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, Options{Quiet: false})
	r.Start("sync: fetching remote manifest", 1)
	r.Done()

	out := buf.String()
	for _, want := range []string{"-> sync: fetching remote manifest", "ok sync: fetching remote manifest"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q, got %q", want, out)
		}
	}
}

func TestTextReporterQuietSuppressesOutput(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, Options{Quiet: true})
	r.Start("download", 100)
	r.Advance(100)
	r.Done()
	if buf.String() != "" {
		t.Fatalf("quiet output: %q", buf.String())
	}
}
