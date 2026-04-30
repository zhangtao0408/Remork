package cli

import (
	"strings"
	"testing"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test-version"})
	out, err := executeCommand(cmd, "version")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "remork test-version" {
		t.Fatalf("output %q", out.String())
	}
}

func TestRootHelpShowsProductCommandLayers(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "help")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	mustContain(t, out.String(), "Must know: init sync status apply run shell")
	mustContain(t, out.String(), "Learn later: pull diff restore log watch")
	mustContain(t, out.String(), "Debug and operations: doctor debug daemon")
}

func TestRunIsVisibleAndExecIsAlias(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "help")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	mustContain(t, out.String(), "run")
	mustNotContain(t, out.String(), "exec")

	execCmd, _, err := cmd.Find([]string{"exec"})
	if err != nil {
		t.Fatalf("find exec: %v", err)
	}
	if execCmd == nil || execCmd.Name() != "exec" {
		t.Fatalf("exec command not found: %#v", execCmd)
	}
	if !execCmd.Hidden {
		t.Fatalf("exec command should be hidden")
	}
}

func TestExecAliasUsesRunPlaceholderHandler(t *testing.T) {
	runCmd := NewRootCommand(Options{Version: "test"})
	_, runErr := executeCommand(runCmd, "run")
	if runErr == nil {
		t.Fatal("run should return the product placeholder error")
	}

	execCmd := NewRootCommand(Options{Version: "test"})
	_, execErr := executeCommand(execCmd, "exec")
	if execErr == nil {
		t.Fatal("exec should return the run placeholder error")
	}

	if execErr.Error() != runErr.Error() {
		t.Fatalf("exec error %q, want run error %q", execErr.Error(), runErr.Error())
	}
}

func TestRootCommandSilencesCobraErrorPrinting(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "run")
	if err == nil {
		t.Fatal("run should return the product placeholder error")
	}

	if out.String() != "" {
		t.Fatalf("expected cobra to leave error output empty, got %q", out.String())
	}
}
