package cli

import "testing"

func TestConflictCommandShowsRecoverySteps(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir()}), "conflict", "a.txt")
	if err != nil {
		t.Fatalf("conflict command: %v", err)
	}
	for _, want := range []string{
		"== Conflict ==",
		"Conflict: a.txt",
		"remork diff -- a.txt",
		"Discard local edits back to synced base",
		"remork restore -- a.txt",
		"remork status",
		"If remote updates remain: remork sync",
		"Then continue or apply as appropriate: remork apply",
	} {
		mustContain(t, out.String(), want)
	}
}

func TestConflictCommandAcceptsDashPrefixedPath(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir()}), "conflict", "--", "-dash.txt")
	if err != nil {
		t.Fatalf("conflict command: %v", err)
	}
	for _, want := range []string{
		"Conflict: -dash.txt",
		"remork diff -- -dash.txt",
		"remork restore -- -dash.txt",
	} {
		mustContain(t, out.String(), want)
	}
}
