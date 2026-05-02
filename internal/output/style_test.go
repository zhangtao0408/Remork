package output

import (
	"bytes"
	"testing"
)

func TestStyleDoesNotColorNonTerminalWriters(t *testing.T) {
	var buf bytes.Buffer
	if got := Success(&buf, "ok"); got != "ok" {
		t.Fatalf("Success on buffer = %q, want plain text", got)
	}
}
