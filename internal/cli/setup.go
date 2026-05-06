package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/tui"
)

type setupScope string

const (
	setupScopeConnectProject setupScope = "connect_project"
	setupScopePrepareServer  setupScope = "prepare_server"
	setupScopeRepair         setupScope = "repair"
)

func addSetupCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up Remork for a server or workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
			if !mode.Wizard {
				return codedCommandError{
					code: exitcode.InvalidUsageOrConfig,
					err:  fmt.Errorf("remork setup requires an interactive terminal"),
					fix:  "run remork setup in a terminal, or use advanced commands such as remork host add and remork init",
				}
			}
			return runSetupScopeMenu(cmd, opts)
		},
	}
	root.AddCommand(cmd)
}

func runSetupScopeMenu(cmd *cobra.Command, opts Options) error {
	_ = opts
	plainRenderer(cmd, false).Section("Setup")
	plainRenderer(cmd, false).List("Choose what to set up", []string{
		"Connect this project",
		"Only prepare a server",
		"Repair an existing setup",
	})
	return nil
}

func setupScopeItems(bound bool) []tui.CommandItem {
	if bound {
		return []tui.CommandItem{
			{Name: "Update an existing server", Description: "Update or verify the daemon used by this workspace", Args: []string{"update"}},
			{Name: "Repair an existing setup", Description: "Check host, daemon, auth, roots, and workspace binding", Args: []string{"repair"}},
			{Name: "Only prepare a server", Description: "Install or update remorkd without binding this directory", Args: []string{"prepare"}},
		}
	}
	return []tui.CommandItem{
		{Name: "Connect this project", Description: "Prepare or choose a daemon, bind this directory, then offer first sync", Args: []string{"connect"}},
		{Name: "Only prepare a server", Description: "Install or update remorkd and configure a host profile", Args: []string{"prepare"}},
		{Name: "Repair an existing setup", Description: "Check host, daemon, auth, roots, and workspace binding", Args: []string{"repair"}},
	}
}
