package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

func executeCommand(cmd *cobra.Command, args ...string) (*bytes.Buffer, error) {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	return &out, cmd.Execute()
}

func executeCommandSplit(cmd *cobra.Command, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	return &stdout, &stderr, cmd.Execute()
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !bytes.Contains([]byte(got), []byte(want)) {
		t.Fatalf("expected output to contain %q, got %q", want, got)
	}
}

func mustNotContain(t *testing.T, got, want string) {
	t.Helper()
	if bytes.Contains([]byte(got), []byte(want)) {
		t.Fatalf("expected output not to contain %q, got %q", want, got)
	}
}
