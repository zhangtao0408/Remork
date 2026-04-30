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

	applyOut := h.runInLocal("apply")
	mustContain(t, applyOut, "applied 1")
	h.assertRemote("a.txt", "local\n")

	h.writeLocal("a.txt", "local-two\n")
	h.writeRemote("a.txt", "remote-two\n")
	errOut, code := h.runInLocalExpectCode(5, "apply")
	mustContain(t, errOut, "conflict")
	h.assertLocal("a.txt", "local-two\n")
	h.assertRemote("a.txt", "remote-two\n")
	if code != 5 {
		t.Fatalf("exit code = %d", code)
	}
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
