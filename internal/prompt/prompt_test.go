package prompt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestQuietReturnsPromptRequired(t *testing.T) {
	_, err := Confirm(Options{Quiet: true}, "download 200MB file?")
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestForceConfirmsWithoutReadingInput(t *testing.T) {
	ok, err := Confirm(Options{Force: true, In: strings.NewReader("")}, "replace file?")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !ok {
		t.Fatal("force should confirm")
	}
}

func TestInteractiveAcceptsY(t *testing.T) {
	var out bytes.Buffer
	ok, err := Confirm(Options{In: strings.NewReader("Y\n"), Out: &out}, "download file?")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !strings.Contains(out.String(), "download file?") {
		t.Fatalf("prompt output = %q", out.String())
	}
}
