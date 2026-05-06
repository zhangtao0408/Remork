package output

import (
	"bytes"
	"os"
	"testing"
)

func TestStyleDoesNotColorNonTerminalWriters(t *testing.T) {
	var buf bytes.Buffer
	if got := Success(&buf, "ok"); got != "ok" {
		t.Fatalf("Success on buffer = %q, want plain text", got)
	}
}

func TestSupportsColorRejectsNonTTYCharacterDevices(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open dev null: %v", err)
	}
	defer f.Close()

	if supportsColor(f) {
		t.Fatal("os.DevNull is a character device but should not be treated as a color terminal")
	}
}
