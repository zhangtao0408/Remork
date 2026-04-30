package output

import (
	"bytes"
	"testing"
)

func TestWriteJSONWritesSingleLineObject(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, map[string]string{"status": "ok"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	if got, want := buf.String(), "{\"status\":\"ok\"}\n"; got != want {
		t.Fatalf("output %q, want %q", got, want)
	}
}
