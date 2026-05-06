package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/cli"
)

func TestRemorkProductApplyUpdatesRemoteAndConflictPreservesLocal(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "base\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "local\n")

	applyOut := h.runInLocal("apply", "--yes")
	mustContain(t, applyOut, "applied 1")
	h.assertRemote("a.txt", "local\n")

	h.writeLocal("a.txt", "local-two\n")
	h.writeRemote("a.txt", "remote-two\n")
	errOut, code := h.runInLocalExpectCode(5, "apply", "--yes")
	mustContain(t, errOut, "conflict")
	mustContain(t, errOut, "inspect: remork conflict -- a.txt")
	h.assertLocal("a.txt", "local-two\n")
	h.assertRemote("a.txt", "remote-two\n")
	if code != 5 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestStatusShowsConflictPathsAndNextSteps(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "base\n")
	h.bindAndSync()

	h.writeLocal("a.txt", "local\n")
	h.writeRemote("a.txt", "remote\n")

	out := h.runInLocal("status")
	mustContain(t, out, "Conflicts: 1")
	mustContain(t, out, "a.txt")
	mustContain(t, out, "remork conflict -- a.txt")
	mustContain(t, out, "Discard local edits back to synced base after review")
	mustContain(t, out, "Then run: remork status")
	mustContain(t, out, "If remote updates remain: remork sync")
}

func TestStatusAndApplyConflictSuggestDashPrefixedPathWithTerminator(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("-dash.txt", "base\n")
	h.bindAndSync()

	h.writeLocal("-dash.txt", "local\n")
	h.writeRemote("-dash.txt", "remote\n")

	statusOut := h.runInLocal("status")
	mustContain(t, statusOut, "-dash.txt")
	mustContain(t, statusOut, "remork conflict -- -dash.txt")
	mustContain(t, statusOut, "remork restore -- -dash.txt")

	errOut, code := h.runInLocalExpectCode(5, "apply", "--yes")
	mustContain(t, errOut, "inspect: remork conflict -- -dash.txt")
	if code != 5 {
		t.Fatalf("exit code = %d", code)
	}

	conflictOut := h.runInLocal("conflict", "--", "-dash.txt")
	mustContain(t, conflictOut, "remork diff -- -dash.txt")
	mustContain(t, conflictOut, "remork restore -- -dash.txt")
	mustContain(t, conflictOut, "If remote updates remain: remork sync")
}

func TestStatusVerboseShowsChangedPaths(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "base\n")
	h.bindAndSync()

	h.writeLocal("a.txt", "local\n")

	out := h.runInLocal("status", "--verbose")
	mustContain(t, out, "Local changes: 1")
	mustContain(t, out, "Changed paths:")
	mustContain(t, out, "a.txt")
}

func newProductHarness(t *testing.T) *cliHarness {
	t.Helper()
	return newCLIHarness(t)
}

func (h *cliHarness) runInLocalExpectCode(want int, args ...string) (string, int) {
	h.t.Helper()
	cmd := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: h.home, WorkingDir: h.local})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil {
		h.t.Fatalf("remork %v succeeded, want exit code %d\noutput:\n%s", args, want, out.String())
	}
	code := 1
	if coded, ok := err.(interface{ ExitCode() int }); ok {
		code = coded.ExitCode()
	}
	if code != want {
		h.t.Fatalf("remork %v exit code = %d, want %d\nerror: %v\noutput:\n%s", args, code, want, err, out.String())
	}
	return out.String() + err.Error(), code
}

func (h *cliHarness) assertRemote(path, want string) {
	h.t.Helper()
	got, err := os.ReadFile(filepath.Join(h.remote, path))
	if err != nil {
		h.t.Fatalf("read remote %s: %v", path, err)
	}
	if string(got) != want {
		h.t.Fatalf("remote %s = %q, want %q", path, got, want)
	}
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q\noutput:\n%s", want, got)
	}
}
