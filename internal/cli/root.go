package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type Options struct {
	Version string
}

const productHelpTemplate = `{{.Short}}

Usage:
  {{.UseLine}}

Must know: init sync status apply run shell
  init        Bind the current directory to a remote workspace
  sync        Sync remote files into the local working copy
  status      Show local, remote, conflict, and large-file state
  apply       Write local changes to the remote after base checks
  run         Run a command in the remote workspace
  shell       Open an interactive remote shell

Learn later: pull diff restore log watch
  pull        Fetch a specific file or directory
  diff        Show local changes against the synced base
  restore     Discard local changes
  log         Show recent remote Remork operations
  watch       Keep syncing from remote events
  host        Manage daemon endpoints
  workspace   Inspect or remove local bindings

Debug and operations: doctor debug daemon
  doctor      Check local and remote readiness
  debug       Inspect daemon APIs and events
  daemon      Install, upgrade, or inspect remorkd

Other:
  version     Print the remork version
`

func NewRootCommand(opts Options) *cobra.Command {
	if opts.Version == "" {
		opts.Version = "dev"
	}

	root := &cobra.Command{
		Use:          "remork",
		Short:        "Control remote workspaces from a local working copy",
		SilenceUsage: true,
	}
	root.SetHelpTemplate(productHelpTemplate)
	addVersionCommand(root, opts.Version)
	addPlaceholderProductCommands(root)
	return root
}

func addVersionCommand(root *cobra.Command, version string) {
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the remork version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "remork "+version)
		},
	})
}

func addPlaceholderProductCommands(root *cobra.Command) {
	names := []string{
		"init",
		"sync",
		"status",
		"apply",
		"run",
		"shell",
		"pull",
		"diff",
		"restore",
		"log",
		"watch",
		"host",
		"workspace",
		"doctor",
		"debug",
		"daemon",
	}
	for _, name := range names {
		name := name
		root.AddCommand(&cobra.Command{
			Use:   name,
			Short: placeholderShort(name),
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("%s command is defined by the Product V1 plan and has no handler in this task", name)
			},
		})
	}

	root.AddCommand(&cobra.Command{
		Use:    "exec",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("exec is a hidden alias for run and has no handler in this task")
		},
	})
}

func placeholderShort(name string) string {
	descriptions := map[string]string{
		"init":      "Bind the current directory to a remote workspace",
		"sync":      "Sync remote files into the local working copy",
		"status":    "Show local, remote, conflict, and large-file state",
		"apply":     "Write local changes to the remote after base checks",
		"run":       "Run a command in the remote workspace",
		"shell":     "Open an interactive remote shell",
		"pull":      "Fetch a specific file or directory",
		"diff":      "Show local changes against the synced base",
		"restore":   "Discard local changes",
		"log":       "Show recent remote Remork operations",
		"watch":     "Keep syncing from remote events",
		"host":      "Manage daemon endpoints",
		"workspace": "Inspect or remove local bindings",
		"doctor":    "Check local and remote readiness",
		"debug":     "Inspect daemon APIs and events",
		"daemon":    "Install, upgrade, or inspect remorkd",
	}
	return descriptions[name]
}
