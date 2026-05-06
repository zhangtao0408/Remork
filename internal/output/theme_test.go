package output

import (
	"bytes"
	"regexp"
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

func TestPlainThemeRendersModernInlineComponents(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorNever})
	r.Section("Sync")
	r.Step("fetching remote manifest")
	r.ProgressBar("download", 2, 4)
	r.KeyValue("workspace", "lab:/repo")
	r.List("Next", []string{"remork status", "remork diff"})
	r.Command("remork apply --yes")
	r.Empty("no hosts configured", "run remork daemon install HOST --root /path")

	got := buf.String()
	for _, want := range []string{
		"== Sync ==",
		"fetching remote manifest",
		"download",
		"2/4",
		"workspace",
		"lab:/repo",
		"Next",
		"remork status",
		"remork apply --yes",
		"no hosts configured",
		"run remork daemon install HOST --root /path",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output should contain %q, got:\n%s", want, got)
		}
	}
}

func TestPlainThemeRendersTablesAndPanels(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorNever})
	r.Panel("Workspace", []string{"host: lab", "remote: /repo"})
	r.Table([]string{"name", "url"}, [][]string{{"lab", "http://daemon:17731"}})

	got := buf.String()
	for _, want := range []string{"Workspace", "host: lab", "name", "url", "lab", "http://daemon:17731"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output should contain %q, got:\n%s", want, got)
		}
	}
}

func TestProgressBarClampsAndAvoidsOverflow(t *testing.T) {
	tests := []struct {
		name    string
		current int64
		total   int64
		want    string
	}{
		{name: "negative current", current: -5, total: 10, want: "0/10"},
		{name: "current above total", current: 12, total: 10, want: "10/10"},
		{name: "huge values", current: 1207260999045565600, total: 2208819584036135628, want: "54%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := NewPlainRenderer(&buf, PlainOptions{Color: ColorNever})
			r.ProgressBar("download", tt.current, tt.total)
			if !strings.Contains(buf.String(), tt.want) {
				t.Fatalf("progress output should contain %q, got %q", tt.want, buf.String())
			}
		})
	}
}

func TestTableAlignsColoredHeadersByVisibleWidth(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorAlways})
	r.Table([]string{"name", "url"}, [][]string{{"longer-name", "http://daemon"}})

	lines := strings.Split(strings.TrimSpace(stripANSI(buf.String())), "\n")
	if len(lines) != 2 {
		t.Fatalf("table should have 2 lines, got %d:\n%s", len(lines), buf.String())
	}
	headerURL := strings.Index(lines[0], "url")
	rowURL := strings.Index(lines[1], "http://daemon")
	if headerURL < 0 || rowURL < 0 || headerURL != rowURL {
		t.Fatalf("url column misaligned after stripping ANSI: header index=%d row index=%d\n%s", headerURL, rowURL, strings.Join(lines, "\n"))
	}
}

func stripANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(s, "")
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
