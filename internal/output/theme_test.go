package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlainThemeRendersStructuredSections(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorNever})

	r.Section("Sync")
	r.Step("fetching remote manifest")
	r.Success("downloaded 2 files")
	r.Warning("1 large file left as metadata")
	r.Error("conflict detected", "run remork conflict path/to/file")

	got := buf.String()
	for _, want := range []string{
		"== Sync ==",
		"-> fetching remote manifest",
		"ok downloaded 2 files",
		"warn 1 large file left as metadata",
		"error conflict detected",
		"next run remork conflict path/to/file",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plain output should contain %q, got:\n%s", want, got)
		}
	}
}

func TestPlainThemeHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorAlways})
	r.Success("done")

	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("NO_COLOR should disable ANSI output, got %q", buf.String())
	}
}

func TestPlainThemeCanForceColorWhenAllowed(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorAlways})
	r.Success("done")

	if !strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("forced color should render ANSI, got %q", buf.String())
	}
}

func TestPlainThemeRendersProductizedActionPlan(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorNever})
	r.ProductTitle("Setup plan", "Remote server will be prepared and verified.")
	r.KeyValue("host", "lab")
	r.ActionList("Actions", []string{"Prepare remote directories", "Copy remorkd binary"})
	r.Next([]string{"remork init lab:/data/project"})

	got := buf.String()
	for _, want := range []string{"Setup plan", "Remote server will be prepared", "host", "Actions", "1. Prepare remote directories", "Next", "remork init"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
