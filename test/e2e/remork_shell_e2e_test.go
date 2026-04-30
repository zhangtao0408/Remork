package e2e

import (
	"bytes"
	"io"
	"os/exec"
	"runtime"
	"testing"

	"remork/internal/cli"
)

func TestRemorkProductShellRemoteOnlySmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	h := newProductHarness(t)
	h.writeRemote("a.txt", "shell\n")
	h.bindAndSync()

	out := h.runShellScript("shell", "--remote-only", "printf 'cat a.txt\\nexit\\n'")
	mustContain(t, out, "shell")
	mustContain(t, out, "Remote-only shell")
}

func TestRemorkProductShellReturnsRemoteExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	h := newProductHarness(t)
	h.bindAndSync()

	out, code := h.runShellScriptExpectCode(7, "shell", "--remote-only", "printf 'exit 7\\n'")
	mustContain(t, out, "Remote-only shell")
	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
}

func (h *cliHarness) runShellScript(args ...string) string {
	h.t.Helper()
	out, err := h.runShellScriptRaw(args...)
	if err != nil && err != io.EOF {
		h.t.Fatalf("remork %v: %v\noutput:\n%s", args[:len(args)-1], err, out)
	}
	return out
}

func (h *cliHarness) runShellScriptExpectCode(want int, args ...string) (string, int) {
	h.t.Helper()
	out, err := h.runShellScriptRaw(args...)
	if err == nil {
		h.t.Fatalf("remork %v succeeded, want exit code %d\noutput:\n%s", args[:len(args)-1], want, out)
	}
	code := 1
	if coded, ok := err.(interface{ ExitCode() int }); ok {
		code = coded.ExitCode()
	}
	if code != want {
		h.t.Fatalf("remork %v exit code = %d, want %d\nerror: %v\noutput:\n%s", args[:len(args)-1], code, want, err, out)
	}
	return out + err.Error(), code
}

func (h *cliHarness) runShellScriptRaw(args ...string) (string, error) {
	h.t.Helper()
	if len(args) < 2 {
		h.t.Fatalf("runShellScript requires command args and a script command")
	}
	scriptCommand := args[len(args)-1]
	cmdArgs := args[:len(args)-1]
	script := exec.Command("sh", "-c", scriptCommand)
	stdin, err := script.StdoutPipe()
	if err != nil {
		h.t.Fatalf("script pipe: %v", err)
	}
	var scriptErr bytes.Buffer
	script.Stderr = &scriptErr
	if err := script.Start(); err != nil {
		h.t.Fatalf("start script: %v", err)
	}
	defer func() {
		if err := script.Wait(); err != nil {
			h.t.Fatalf("script failed: %v\nstderr:\n%s", err, scriptErr.String())
		}
	}()

	root := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: h.home, WorkingDir: h.local})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(stdin)
	root.SetArgs(cmdArgs)
	err = root.Execute()
	return out.String(), err
}
