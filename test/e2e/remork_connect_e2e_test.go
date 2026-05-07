package e2e

import "testing"

func TestRemorkConnectExistingDaemonWorkflow(t *testing.T) {
	h := newCLIHarness(t)
	h.writeRemote("a.txt", "hello\n")

	h.runInLocal("connect", "--url", h.serverURL, "--host", "lab", "--workspace-path", "", "--first-sync=false", "--non-interactive")
	h.runInLocal("sync")

	out := h.runInLocal("run", "--remote-only", "cat a.txt")
	mustContain(t, out, "hello")
}
