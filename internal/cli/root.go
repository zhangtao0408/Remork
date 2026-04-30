package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"remork/internal/api"
	"remork/internal/config"
)

type Options struct {
	Version     string
	HomeDir     string
	WorkingDir  string
	DaemonProbe DaemonProbe
}

type DaemonProbe interface {
	Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error)
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
	if opts.HomeDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			opts.HomeDir = home
		}
	}
	if opts.WorkingDir == "" {
		wd, err := os.Getwd()
		if err == nil {
			opts.WorkingDir = wd
		}
	}

	root := &cobra.Command{
		Use:           "remork",
		Short:         "Control remote workspaces from a local working copy",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetHelpTemplate(productHelpTemplate)
	addVersionCommand(root, opts.Version)
	addHostCommand(root, opts)
	addInitCommand(root, opts)
	addPlaceholderProductCommands(root)
	return root
}

func configStore(opts Options) config.Store {
	return config.NewStore(filepath.Join(opts.HomeDir, ".remork"))
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
		"workspace",
		"doctor",
		"debug",
		"daemon",
	}
	var runCmd *cobra.Command
	for _, name := range names {
		name := name
		cmd := &cobra.Command{
			Use:   name,
			Short: placeholderShort(name),
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("%s command is defined by the Product V1 plan and has no handler in this task", name)
			},
		}
		if name == "run" {
			runCmd = cmd
		}
		root.AddCommand(cmd)
	}

	root.AddCommand(&cobra.Command{
		Use:    "exec",
		Hidden: true,
		Args:   runCmd.Args,
		Run:    runCmd.Run,
		RunE:   runCmd.RunE,
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
