package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"remork/internal/api"
	"remork/internal/auth"
	remorkclient "remork/internal/client"
	"remork/internal/config"
	"remork/internal/ops"
)

type Options struct {
	Version     string
	HomeDir     string
	WorkingDir  string
	DaemonProbe DaemonProbe
}

type DaemonProbe interface {
	Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error)
	Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error)
	Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error)
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

Learn later: pull diff restore conflict log watch
  pull        Fetch a specific file or directory
  diff        Show local changes against the synced base
  restore     Discard local changes
  conflict    Show conflict recovery steps for a path
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
{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}{{if .HasAvailableInheritedFlags}}
Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}
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
	if opts.DaemonProbe == nil {
		opts.DaemonProbe = httpDaemonProbe{}
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
	addSyncCommand(root, opts)
	addStatusCommand(root, opts)
	addDiffCommand(root, opts)
	addRestoreCommand(root, opts)
	addConflictCommand(root, opts)
	addApplyCommand(root, opts)
	addPullCommand(root, opts)
	addRunCommand(root, opts)
	addShellCommand(root, opts)
	addLogCommand(root, opts)
	addWatchCommand(root, opts)
	addDoctorCommand(root, opts)
	addDebugCommand(root, opts)
	addDaemonCommand(root, opts)
	addWorkspaceCommand(root, opts)
	addPlaceholderProductCommands(root)
	return root
}

func configStore(opts Options) (config.Store, error) {
	if opts.HomeDir == "" {
		return config.Store{}, fmt.Errorf("home directory is required for remork config")
	}
	return config.NewStore(filepath.Join(opts.HomeDir, ".remork")), nil
}

type httpDaemonProbe struct {
	client *http.Client
}

func (p httpDaemonProbe) Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error) {
	if clientID == "" {
		clientID = "remork-cli"
	}
	token, err := auth.TokenFromEnv(host.TokenEnv)
	if err != nil {
		return api.StatusResponse{}, err
	}
	c := p.clientFor(host, clientID, token)
	return c.StatusContext(ctx)
}

func (p httpDaemonProbe) Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error) {
	token, err := auth.TokenFromEnv(host.TokenEnv)
	if err != nil {
		return api.ManifestResponse{}, err
	}
	c := p.clientFor(host, cfg.ClientID, token)
	return c.ManifestContext(ctx, root, ".")
}

func (p httpDaemonProbe) Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error) {
	token, err := auth.TokenFromEnv(host.TokenEnv)
	if err != nil {
		return nil, err
	}
	c := p.clientFor(host, cfg.ClientID, token)
	return c.OperationsContext(ctx, root, limit)
}

func (p httpDaemonProbe) clientFor(host config.Host, clientID string, token string) remorkclient.Client {
	return remorkclient.NewWithOptions(remorkclient.Options{
		BaseURL:  host.URL,
		ClientID: clientID,
		Token:    token,
		HTTP:     p.client,
		NoProxy:  host.NoProxy,
	})
}

func clientForHost(host config.Host, cfg config.Config, token string) remorkclient.Client {
	return remorkclient.NewWithOptions(remorkclient.Options{
		BaseURL:  host.URL,
		ClientID: cfg.ClientID,
		Token:    token,
		NoProxy:  host.NoProxy,
	})
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
	names := []string{}
	for _, name := range names {
		name := name
		cmd := &cobra.Command{
			Use:   name,
			Short: placeholderShort(name),
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("%s command is defined by the Product V1 plan and has no handler in this task", name)
			},
		}
		root.AddCommand(cmd)
	}
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
		"conflict":  "Show conflict recovery steps for a path",
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
