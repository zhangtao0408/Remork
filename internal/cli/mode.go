package cli

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"remork/internal/output"
)

type interactionRequest struct {
	TTY            bool
	MissingInput   bool
	JSON           bool
	Quiet          bool
	Yes            bool
	NonInteractive bool
}

type interactionMode struct {
	Wizard     bool
	RichOutput bool
}

func decideInteractionMode(req interactionRequest) interactionMode {
	if req.NonInteractive || req.JSON || req.Quiet || req.Yes || !req.TTY {
		return interactionMode{}
	}
	return interactionMode{
		Wizard:     req.MissingInput,
		RichOutput: true,
	}
}

func commandInteractionMode(cmd *cobra.Command, req interactionRequest) interactionMode {
	req.TTY = req.TTY || commandHasTTY(cmd)
	req.NonInteractive = req.NonInteractive || boolFlag(cmd, "non-interactive")
	return decideInteractionMode(req)
}

func commandColorMode(cmd *cobra.Command) output.ColorMode {
	value := string(output.ColorAuto)
	if flag := cmd.Root().PersistentFlags().Lookup("color"); flag != nil {
		value = flag.Value.String()
	}
	mode, err := output.ParseColorMode(value)
	if err != nil {
		return output.ColorAuto
	}
	return mode
}

func validateGlobalFlags(cmd *cobra.Command, args []string) error {
	value := string(output.ColorAuto)
	if flag := cmd.Root().PersistentFlags().Lookup("color"); flag != nil {
		value = flag.Value.String()
	}
	if _, err := output.ParseColorMode(value); err != nil {
		return codedCommandError{code: 2, err: err}
	}
	return nil
}

func boolFlag(cmd *cobra.Command, name string) bool {
	flag := cmd.Flag(name)
	if flag == nil {
		return false
	}
	return flag.Value.String() == "true"
}

func commandHasTTY(cmd *cobra.Command) bool {
	return isTerminal(cmd.InOrStdin()) && isTerminal(cmd.OutOrStdout())
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func plainRenderer(cmd *cobra.Command, quiet bool) *output.PlainRenderer {
	return output.NewPlainRenderer(cmd.OutOrStdout(), output.PlainOptions{
		Color: commandColorMode(cmd),
		Quiet: quiet,
	})
}

func plainErrRenderer(cmd *cobra.Command, quiet bool) *output.PlainRenderer {
	return output.NewPlainRenderer(cmd.ErrOrStderr(), output.PlainOptions{
		Color: commandColorMode(cmd),
		Quiet: quiet,
	})
}

func requireInteractiveTerminal(cmd *cobra.Command, purpose string) error {
	if boolFlag(cmd, "non-interactive") {
		return fmt.Errorf("%s requires a terminal; --non-interactive is for scripted commands such as remork run", purpose)
	}
	if !commandHasTTY(cmd) {
		return fmt.Errorf("%s requires a terminal; use remork run for non-interactive automation", purpose)
	}
	return nil
}
