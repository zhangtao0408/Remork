package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/api"
	"remork/internal/auth"
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
	if opts.DaemonProbe == nil {
		opts.DaemonProbe = httpDaemonProbe{client: http.DefaultClient}
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
	addApplyCommand(root, opts)
	addPullCommand(root, opts)
	addRunCommand(root, opts)
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
	u, err := url.Parse(host.URL)
	if err != nil {
		return api.StatusResponse{}, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return api.StatusResponse{}, err
	}
	if clientID == "" {
		clientID = "remork-cli"
	}
	req.Header.Set(api.HeaderClientID, clientID)
	token, err := auth.TokenFromEnv(host.TokenEnv)
	if err != nil {
		return api.StatusResponse{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return api.StatusResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return api.StatusResponse{}, fmt.Errorf("daemon status failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var status api.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return api.StatusResponse{}, err
	}
	return status, nil
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
		"shell",
		"log",
		"watch",
		"workspace",
		"doctor",
		"debug",
		"daemon",
	}
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
