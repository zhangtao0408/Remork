package cli

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"remork/internal/tui"
)

func runTUIForm(cmd *cobra.Command, title string, fields []tui.Field) (map[string]string, error) {
	values, err := tui.RunForm(
		tui.NewFormModel(title, fields),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.ErrOrStderr()),
	)
	if errors.Is(err, tui.ErrCanceled) {
		return nil, fmt.Errorf("%s canceled", title)
	}
	if errors.Is(err, tui.ErrIncomplete) {
		return nil, fmt.Errorf("%s was not completed", title)
	}
	return values, err
}
