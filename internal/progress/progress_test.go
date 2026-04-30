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
