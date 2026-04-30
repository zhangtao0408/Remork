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

func (h *cliHarness) runShellScript(args ...string) string {
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
	if err := root.Execute(); err != nil && err != io.EOF {
		h.t.Fatalf("remork %v: %v\noutput:\n%s", cmdArgs, err, out.String())
	}
	return out.String()
}
