package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"remork/internal/api"
	"remork/internal/auth"
	remorkclient "remork/internal/client"
	"remork/internal/config"
	"remork/internal/ops"
	"remork/internal/tui"
	"remork/internal/workspace"
)

type Options struct {
	Version       string
	HomeDir       string
	WorkingDir    string
	DaemonProbe   DaemonProbe
	CommandRunner commandRunner
}

type DaemonProbe interface {
	Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error)
	Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error)
	Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error)
}

const productHelpTemplate = `{{.Short}}

Usage:
  {{.UseLine}}

Setup:
  setup       Set up Remork for a server or workspace

Daily:
  sync        Sync remote files into the local working copy
  status      Show local, remote, conflict, and large-file state
  diff        Show local changes against the synced base
  apply       Write local changes to the remote after base checks
  pull        Fetch a specific file or directory
  run         Run a command in the remote workspace
  shell       Open an interactive remote shell

Observe:
  log         Show recent remote Remork operations
  watch       Keep syncing from remote events

Diagnose:
  doctor      Check local and remote readiness

Advanced:
  daemon      Install, upgrade, or inspect remorkd
  host        Manage daemon endpoints
  workspace   Inspect or remove local bindings
  debug       Inspect daemon APIs and events
  init        Bind the current directory to a remote workspace

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

const commandHelpTemplate = `{{if .Long}}{{.Long}}{{else}}{{.Short}}{{end}}

Usage:
  {{.UseLine}}
{{if .HasExample}}
Examples:
{{.Example}}
{{end}}{{if .HasAvailableSubCommands}}
Commands:
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}
{{end}}{{if .HasAvailableLocalFlags}}
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
		Use:               "remork",
		Short:             "Control remote workspaces from a local working copy",
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: validateGlobalFlags,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
			if !mode.Wizard {
				return cmd.Help()
			}
			return runRootCommandMenu(cmd, opts)
		},
	}
	root.PersistentFlags().Bool("non-interactive", false, "Disable interactive prompts and TUI rendering")
	root.PersistentFlags().String("color", "auto", "Color output: auto, always, or never")
	root.SetHelpTemplate(productHelpTemplate)
	addVersionCommand(root, opts.Version)
	addSetupCommand(root, opts)
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
	applyDetailedCommandDocs(root)
	applySubcommandHelpTemplate(root)
	return root
}

func runRootCommandMenu(cmd *cobra.Command, opts Options) error {
	model := tui.NewCommandMenu("Remork command menu", rootCommandItems(workspaceIsBound(opts)))
	model.Color = commandColorMode(cmd)
	menu, err := tui.RunCommandMenu(model,
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.ErrOrStderr()),
	)
	if err != nil {
		return err
	}
	if menu.Canceled() || !menu.Submitted() {
		return nil
	}
	args := menu.SelectedArgs()
	if len(args) == 0 {
		return nil
	}
	cmd.Root().SetArgs(args)
	return cmd.Root().ExecuteContext(cmd.Context())
}

func rootCommandItems(bound bool) []tui.CommandItem {
	setup := []tui.CommandItem{
		{Group: "Setup", Name: "setup", Description: "Set up Remork for a server or workspace", Args: []string{"setup"}},
	}
	daily := []tui.CommandItem{
		{Group: "Daily", Name: "sync", Description: "Pull remote changes into this working copy", Args: []string{"sync"}},
		{Group: "Daily", Name: "status", Description: "Inspect local edits, remote updates, conflicts, and large files", Args: []string{"status"}},
		{Group: "Daily", Name: "diff", Description: "Review local edits before applying", Args: []string{"diff"}},
		{Group: "Daily", Name: "apply", Description: "Write reviewed local edits back to the remote", Args: []string{"apply"}},
		{Group: "Daily", Name: "pull", Description: "Fetch one file or directory, including large files", Args: []string{"pull"}, HelpOnly: true},
		{Group: "Daily", Name: "run", Description: "Run a non-interactive remote command", Args: []string{"run"}, HelpOnly: true},
		{Group: "Daily", Name: "shell", Description: "Open an interactive remote shell", Args: []string{"shell"}},
	}
	observe := []tui.CommandItem{
		{Group: "Observe", Name: "log", Description: "Show recent remote Remork operations", Args: []string{"log"}},
		{Group: "Observe", Name: "watch", Description: "Follow remote workspace events and sync updates", Args: []string{"watch"}},
	}
	diagnose := []tui.CommandItem{
		{Group: "Diagnose", Name: "doctor", Description: "Check local config, daemon reachability, and workspace APIs", Args: []string{"doctor"}},
	}
	advanced := []tui.CommandItem{
		{Group: "Advanced", Name: "daemon status", Description: "Inspect remorkd version, allowed roots, auth, and threshold", Args: []string{"daemon", "status"}, HelpOnly: true},
		{Group: "Advanced", Name: "daemon install", Description: "Install remorkd over SSH", Args: []string{"daemon", "install"}, HelpOnly: true},
		{Group: "Advanced", Name: "daemon upgrade", Description: "Upgrade remorkd over SSH", Args: []string{"daemon", "upgrade"}, HelpOnly: true},
		{Group: "Advanced", Name: "host list", Description: "List configured daemon endpoints", Args: []string{"host", "list"}},
		{Group: "Advanced", Name: "workspace", Description: "Inspect this local workspace binding", Args: []string{"workspace"}},
		{Group: "Advanced", Name: "init", Description: "Bind this directory manually", Args: []string{"init"}, HelpOnly: true},
	}
	if !bound {
		return append(append(append(append(setup, daily...), observe...), diagnose...), advanced...)
	}
	return append(append(append(append(daily, setup...), observe...), diagnose...), advanced...)
}

func workspaceIsBound(opts Options) bool {
	_, _, err := workspace.ResolveFrom(opts.WorkingDir)
	return err == nil
}

type commandDoc struct {
	long    string
	example string
}

func applyDetailedCommandDocs(root *cobra.Command) {
	docs := map[string]commandDoc{
		"setup": {
			long: `Set up Remork through a guided product flow.

When to use:
  Use setup when you are connecting a project for the first time, preparing a
  server for Remork, updating an existing daemon, or repairing local/remote
  configuration.

Interactive:
  setup opens a scoped menu and then shows a review plan before it changes
  host, daemon, or workspace state.

Automation:
  Use the advanced host, daemon, init, and workspace commands directly when you
  need a scriptable non-interactive path.`,
			example: `  remork setup
  remork host add my-lab --url http://server:17731
  remork init my-lab:/absolute/remote/path --non-interactive`,
		},
		"init": {
			long: `Bind the current local directory to a remote workspace managed by remorkd.

When to use:
  Use init once per local working copy before sync, status, run, shell, or apply.

Interactive:
  In a terminal, remork init without arguments opens a guided prompt.

Automation:
  Pass HOST:/absolute/remote/path and --non-interactive to avoid prompts.`,
			example: `  remork init
  remork init HOST:/absolute/remote/path --non-interactive
  remork sync`,
		},
		"sync": {
			long: `Download remote changes into the local working copy.

When to use:
  Run sync before editing, before reading local files, or after remote commands
  may have changed the workspace.

Large files:
  Files above the large-file threshold are represented as .meta placeholders
  unless you explicitly pull them.`,
			example: `  remork sync
  remork sync src/
  remork sync --json
  remork sync --force`,
		},
		"status": {
			long: `Show whether the local working copy is clean, has local edits, has remote updates, conflicts, or large-file placeholders.

When to use:
  Run status before apply or when sync/run refuses to continue.`,
			example: `  remork status
  remork status --verbose
  remork status --json`,
		},
		"diff": {
			long: `Show local file changes compared with the last synced base snapshot.

When to use:
  Review exactly what apply would send to the remote.`,
			example: `  remork diff
  remork diff src/main.go`,
		},
		"restore": {
			long: `Discard local changes and restore files from the last synced base snapshot.

When to use:
  Use after reviewing a conflict or local edit that should not be applied.`,
			example: `  remork restore a.txt
  remork restore .`,
		},
		"conflict": {
			long: `Print recovery guidance for a conflicted path.

When to use:
  Use when status reports conflicts after sync, pull, or apply.`,
			example: `  remork conflict a.txt
  remork conflict -- --dash-prefixed-file`,
		},
		"apply": {
			long: `Send reviewed local changes to the remote workspace.

When to use:
  Edit locally, inspect with diff/status, then apply to update the remote.

Safety:
  Apply checks the synced base first. In scripts or JSON mode, pass --yes to
  confirm that the reviewed changes should be written.`,
			example: `  remork diff
  remork apply
  remork apply --yes --non-interactive
  remork apply src/main.go --yes`,
		},
		"pull": {
			long: `Fetch a specific remote file or directory into the local working copy.

When to use:
  Use pull for large files represented by .meta placeholders or when you only
  need one path instead of a full sync.`,
			example: `  remork pull model.tar.gz
  remork pull --force model.tar.gz
  remork pull HOST:/absolute/remote/path/to/file`,
		},
		"run": {
			long: `Run a command inside the bound remote workspace.

When to use:
  Use run for non-interactive commands such as tests, build steps, grep, or
  one-shot diagnostics.

Safety:
  By default run checks local/remote state and syncs remote updates when safe.
  Use --remote-only only when you intentionally want to ignore local edits.

Output:
  Output is replayed after the remote command completes. For a live interactive
  terminal, use remork shell.`,
			example: `  remork run -- pwd
  remork run "pytest -q"
  remork run --timeout 30s "go test ./..."
  remork run --remote-only -- nvidia-smi`,
		},
		"shell": {
			long: `Open an interactive shell inside the bound remote workspace.

When to use:
  Use shell when a human needs an exploratory terminal session on the remote
  workspace. Use run for scripts and Agent automation.`,
			example: `  remork shell
  remork shell --no-sync-check`,
		},
		"log": {
			long: `Show recent operation records stored by the remote daemon for this workspace.

When to use:
  Use log to see which Remork clients requested sync, apply, run, pull, and
  other daemon operations.`,
			example: `  remork log
  remork log --limit 50
  remork log --json`,
		},
		"watch": {
			long: `Watch remote workspace events and keep the local working copy updated.

When to use:
  Use watch during active remote editing or long-running remote workflows where
  you want local files to follow remote changes.`,
			example: `  remork watch`,
		},
		"doctor": {
			long: `Check local configuration, workspace binding, daemon reachability, advertised roots, and workspace APIs.

When to use:
  Run doctor when init, sync, status, apply, run, or shell fail and you need an
  actionable diagnosis.`,
			example: `  remork doctor
  remork doctor --json`,
		},
		"host": {
			long: `Manage named daemon endpoints used by workspace bindings.

When to use:
  Add a host after installing remorkd, list saved hosts, or remove stale
  endpoints.`,
			example: `  remork host add my-lab --url http://daemon-host:17731 --token-env REMORK_TOKEN
  remork host list
  remork host remove my-lab`,
		},
		"host add": {
			long: `Save a named daemon endpoint in the local Remork config.

When to use:
  Run this after remorkd is installed or when a daemon URL/token variable
  changes.`,
			example: `  remork host add my-lab --url http://daemon-host:17731
  remork host add my-lab --url http://daemon-host:17731 --token-env REMORK_TOKEN --no-proxy
  remork daemon status my-lab`,
		},
		"host list": {
			long: `List daemon endpoints saved on this machine.

When to use:
  Use this to confirm which host names init can reference.`,
			example: `  remork host list
  remork host list --json`,
		},
		"host remove": {
			long: `Remove a saved daemon endpoint from local Remork config.

When to use:
  Use this when a daemon endpoint is no longer valid or should not be used.`,
			example: `  remork host remove my-lab`,
		},
		"workspace": {
			long: `Inspect local workspace bindings and remove the current binding marker.

When to use:
  Use workspace commands when a directory is bound to the wrong workspace root or
  when you need to audit local bindings.`,
			example: `  remork workspace
  remork workspace list
  remork workspace remove`,
		},
		"workspace list": {
			long: `List local workspaces registered in Remork config.

When to use:
  Use this to find existing local directories and their workspace roots.`,
			example: `  remork workspace list
  remork workspace list --json`,
		},
		"workspace remove": {
			long: `Remove the .remork binding marker from the current directory.

When to use:
  Use this before rebinding a directory to a different remote workspace.`,
			example: `  remork workspace remove
  remork init HOST:/absolute/remote/path`,
		},
		"daemon": {
			long: `Install, upgrade, or inspect the remote remorkd daemon.

When to use:
  Use daemon commands to place an offline remorkd binary on a server, verify
  version/root/auth state, or preview an installation plan with --dry-run.
  Without --dry-run, install and upgrade show the plan and ask for confirmation.
  Pass -y/--yes to execute without prompting. Interactive daemon forms expose
  all deployment parameters on one screen and reopen with previous values after
  pre-execution validation errors.`,
			example: `  remork daemon install my-lab --root /absolute/allowed/root
  remork daemon install my-lab --root /absolute/allowed/root --dry-run
  remork daemon install my-lab --root /absolute/allowed/root -y --verify
  remork daemon status my-lab`,
		},
		"daemon status": {
			long: `Show daemon version, platform, allowed roots, large-file threshold, watch support, and auth state.

When to use:
  Run after daemon install/upgrade or when init says a root is not advertised.`,
			example: `  remork daemon status my-lab`,
		},
		"daemon install": {
			long: `Prepare or execute an offline remorkd installation on a remote server.

When to use:
  Use install when the server cannot build Remork itself. The command copies a
  prebuilt daemon binary over SSH and can start it under the remote user's home.
  Without --dry-run, it asks for confirmation before executing; pass -y/--yes
  for scripts. The interactive form includes roots, URL, auth, verify, dry-run,
  and unauthenticated bind settings.

Token-first:
  Prefer --token-file plus --token-env for shared VPNs or multi-user networks.`,
			example: `  remork daemon install my-lab --root /absolute/allowed/root
  remork daemon install my-lab --root /absolute/allowed/root --dry-run
  remork daemon install my-lab --root /absolute/allowed/root --ssh user@server --url http://daemon-host:17731 --token-file ~/.remork/remork.token --token-env REMORK_TOKEN -y --verify`,
		},
		"daemon upgrade": {
			long: `Replace an existing remote remorkd binary with the current release binary.

When to use:
  Use upgrade when daemon status shows an older version or install verification
  reports a version mismatch. Use --dry-run when you only want to preview the
  generated SSH/SCP plan. Without --dry-run, it asks for confirmation before
  executing; pass -y/--yes for scripts.`,
			example: `  remork daemon upgrade my-lab --root /absolute/allowed/root
  remork daemon upgrade my-lab --root /absolute/allowed/root --dry-run
  remork daemon upgrade my-lab --root /absolute/allowed/root -y --verify`,
		},
		"debug": {
			long: `Inspect low-level daemon APIs and event streams.

When to use:
  Use debug commands when doctor is not enough and you need raw manifest,
  event, or API probe details.`,
			example: `  remork debug api
  remork debug manifest --json
  remork debug events`,
		},
		"debug manifest": {
			long: `Fetch and print the remote manifest for the bound workspace.

When to use:
  Use this to inspect what the daemon currently advertises for files, hashes,
  directories, and large-file placeholders.`,
			example: `  remork debug manifest
  remork debug manifest --json`,
		},
		"debug events": {
			long: `Stream remote workspace events as JSON lines.

When to use:
  Use this to verify watch/events behavior or diagnose whether daemon-side file
  changes are being observed.`,
			example: `  remork debug events`,
		},
		"debug api": {
			long: `Probe the daemon status, manifest, and operations APIs for the bound workspace.

When to use:
  Use this when doctor points to daemon API trouble and you need individual
  endpoint results.`,
			example: `  remork debug api`,
		},
	}
	applyDocsRecursive(root, docs)
}

func applyDocsRecursive(cmd *cobra.Command, docs map[string]commandDoc) {
	for _, child := range cmd.Commands() {
		if doc, ok := docs[commandDocKey(child)]; ok {
			child.Long = doc.long
			child.Example = doc.example
		}
		applyDocsRecursive(child, docs)
	}
}

func commandDocKey(cmd *cobra.Command) string {
	return strings.TrimPrefix(cmd.CommandPath(), "remork ")
}

func applySubcommandHelpTemplate(root *cobra.Command) {
	for _, cmd := range root.Commands() {
		applyCommandHelpTemplate(cmd)
	}
}

func applyCommandHelpTemplate(cmd *cobra.Command) {
	cmd.SetHelpTemplate(commandHelpTemplate)
	for _, child := range cmd.Commands() {
		applyCommandHelpTemplate(child)
	}
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
	token, err := tokenFromHost(host)
	if err != nil {
		return api.StatusResponse{}, err
	}
	c := p.clientFor(host, clientID, token)
	return c.StatusContext(ctx)
}

func (p httpDaemonProbe) Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error) {
	token, err := tokenFromHost(host)
	if err != nil {
		return api.ManifestResponse{}, err
	}
	c := p.clientFor(host, cfg.ClientID, token)
	return c.ManifestContext(ctx, root, ".")
}

func (p httpDaemonProbe) Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error) {
	token, err := tokenFromHost(host)
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

func tokenSourceFromHost(host config.Host) auth.TokenSource {
	return auth.TokenSource{Env: host.TokenEnv, File: host.TokenFile}
}

func tokenFromHost(host config.Host) (string, error) {
	return auth.TokenFromSource(tokenSourceFromHost(host))
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
