package cli

import (
	"os"
	"testing"
)

func TestInteractionModeUsesWizardOnlyForTTYAndMissingInput(t *testing.T) {
	mode := decideInteractionMode(interactionRequest{
		TTY:          true,
		MissingInput: true,
	})

	if !mode.Wizard || !mode.RichOutput {
		t.Fatalf("mode = %#v, want wizard and rich output", mode)
	}
}

func TestInteractionModeKeepsJSONAndQuietNonInteractive(t *testing.T) {
	tests := []interactionRequest{
		{TTY: true, MissingInput: true, JSON: true},
		{TTY: true, MissingInput: true, Quiet: true},
		{TTY: true, MissingInput: true, Yes: true},
		{TTY: true, MissingInput: true, NonInteractive: true},
		{TTY: false, MissingInput: true},
	}

	for _, tt := range tests {
		mode := decideInteractionMode(tt)
		if mode.Wizard || mode.RichOutput {
			t.Fatalf("decideInteractionMode(%#v) = %#v, want plain mode", tt, mode)
		}
	}
}

func TestInteractionModeKeepsCompleteArgumentCallsOutOfWizard(t *testing.T) {
	mode := decideInteractionMode(interactionRequest{
		TTY:          true,
		MissingInput: false,
	})

	if mode.Wizard {
		t.Fatalf("mode = %#v, want no wizard for complete argument calls", mode)
	}
	if !mode.RichOutput {
		t.Fatalf("mode = %#v, want rich output for TTY text calls", mode)
	}
}

func TestRootCommandRegistersNonInteractiveAndColorFlags(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	if flag := cmd.PersistentFlags().Lookup("non-interactive"); flag == nil {
		t.Fatal("root command should register --non-interactive")
	}
	if flag := cmd.PersistentFlags().Lookup("color"); flag == nil {
		t.Fatal("root command should register --color")
	}
}

func TestDevNullIsNotDetectedAsTTY(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open dev null: %v", err)
	}
	defer f.Close()

	if isTerminal(f) {
		t.Fatal("os.DevNull must not be treated as an interactive TTY")
	}
}

func TestInvalidColorFlagFails(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	_, err := executeCommand(cmd, "--color=bogus", "version")
	if err == nil {
		t.Fatal("invalid --color value should fail")
	}
	mustContain(t, err.Error(), "invalid color mode")
}

func TestColorFlagIsReadFromSubcommands(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	cmd.SetArgs([]string{"--color=always", "sync"})
	if err := cmd.ParseFlags([]string{"--color=always"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if got := commandColorMode(cmd); got != "always" {
		t.Fatalf("root commandColorMode = %q, want always", got)
	}
}

func TestInitWithoutArgsInPlainModeReturnsHelpfulError(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir()})

	_, err := executeCommand(cmd, "init", "--non-interactive")
	if err == nil {
		t.Fatal("init without args in non-interactive mode should fail")
	}
	if coded, ok := err.(interface{ ExitCode() int }); !ok || coded.ExitCode() != 2 {
		t.Fatalf("exit code = %v, want 2", err)
	}
	mustContain(t, err.Error(), "requires host:/absolute/path")
	mustContain(t, err.Error(), "run remork init from an interactive terminal")
}

func TestApplyRegistersYesFlag(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	applyCmd, _, err := cmd.Find([]string{"apply"})
	if err != nil {
		t.Fatalf("find apply: %v", err)
	}
	if flag := applyCmd.Flags().Lookup("yes"); flag == nil {
		t.Fatal("apply should register --yes")
	}
}
