package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/prompt"
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
					err:  fmt.Errorf("remork setup requires an interactive terminal; use remork host add and remork init for non-interactive setup"),
					fix:  "run remork setup in a terminal, or use advanced commands such as remork host add and remork init",
				}
			}
			return runSetupScopeMenu(cmd, opts)
		},
	}
	root.AddCommand(cmd)
}

func runSetupScopeMenu(cmd *cobra.Command, opts Options) error {
	model := tui.NewCommandMenu("Remork setup", setupScopeItems(workspaceIsBound(opts)))
	model.Color = commandColorMode(cmd)
	menu, err := tui.RunCommandMenu(model, tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.ErrOrStderr()))
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
	switch args[0] {
	case "prepare":
		return runSetupPrepareServer(cmd, opts)
	case "connect":
		return runSetupConnectProject(cmd, opts)
	case "update":
		return runSetupUpdateServer(cmd, opts)
	case "repair":
		return runSetupRepair(cmd, opts)
	default:
		return fmt.Errorf("unknown setup scope %q", args[0])
	}
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

func setupPrepareServerSpecs(values map[string]string) (DaemonDeploySpec, HostConfigSpec, error) {
	verify, err := parseDaemonDeployBool(values["verify"], "verify")
	if err != nil {
		return DaemonDeploySpec{}, HostConfigSpec{}, err
	}
	noProxy, err := parseDaemonDeployBool(values["no_proxy"], "no proxy")
	if err != nil {
		return DaemonDeploySpec{}, HostConfigSpec{}, err
	}
	host := strings.TrimSpace(values["host"])
	url := strings.TrimSpace(values["url"])
	spec := DaemonDeploySpec{
		Action:    "install",
		HostName:  host,
		SSHTarget: strings.TrimSpace(values["ssh"]),
		Roots:     splitDaemonDeployRoots(values["roots"]),
		Addr:      strings.TrimSpace(values["addr"]),
		LocalBin:  strings.TrimSpace(values["local_bin"]),
		RemoteBin: strings.TrimSpace(values["remote_bin"]),
		URL:       url,
		TokenEnv:  strings.TrimSpace(values["token_env"]),
		NoProxy:   noProxy,
		Verify:    verify,
		Execute:   true,
	}
	return spec, HostConfigSpec{Name: host, URL: url, TokenEnv: spec.TokenEnv, NoProxy: noProxy}, nil
}

func setupPrepareServerFields(initial map[string]string) []tui.Field {
	if initial == nil {
		initial = map[string]string{}
	}
	return []tui.Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab", Initial: initial["host"]},
		{Key: "ssh", Label: "SSH target", Placeholder: "user@server", Initial: initial["ssh"]},
		{Key: "roots", Label: "Allowed roots", Placeholder: "/absolute/allowed/root", Initial: initial["roots"]},
		{Key: "url", Label: "Daemon URL", Placeholder: "http://server:17731", Initial: initial["url"]},
		{Key: "addr", Label: "Listen addr", Placeholder: "0.0.0.0:17731", Initial: firstNonEmpty(initial["addr"], "0.0.0.0:17731")},
		{Key: "local_bin", Label: "Local binary", Placeholder: "auto", Initial: initial["local_bin"]},
		{Key: "remote_bin", Label: "Remote binary", Placeholder: ".local/bin/remorkd", Initial: firstNonEmpty(initial["remote_bin"], ".local/bin/remorkd")},
		{Key: "token_env", Label: "Token env", Placeholder: "REMORK_TOKEN", Initial: initial["token_env"]},
		{Key: "no_proxy", Label: "Bypass proxy y/N", Placeholder: "no", Initial: firstNonEmpty(initial["no_proxy"], "no")},
		{Key: "verify", Label: "Verify y/N", Placeholder: "yes", Initial: firstNonEmpty(initial["verify"], "yes")},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func setupConnectProjectSpec(localRoot string, values map[string]string) (WorkspaceBindSpec, bool, error) {
	firstSync, err := parseDaemonDeployBool(values["first_sync"], "first sync")
	if err != nil {
		return WorkspaceBindSpec{}, false, err
	}
	spec := WorkspaceBindSpec{
		HostName:   strings.TrimSpace(values["host"]),
		RemoteRoot: strings.TrimSpace(values["remote_root"]),
		LocalRoot:  localRoot,
	}
	if spec.HostName == "" || spec.RemoteRoot == "" {
		return WorkspaceBindSpec{}, false, fmt.Errorf("host and remote workspace root are required")
	}
	return spec, firstSync, nil
}

func setupUpdateServerSpec(values map[string]string) (DaemonDeploySpec, error) {
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		return DaemonDeploySpec{}, err
	}
	spec.Action = "upgrade"
	return spec, nil
}

func renderSetupPlan(w interface{ Write([]byte) (int, error) }, color output.ColorMode, plan OperationPlan) {
	r := output.NewPlainRenderer(w, output.PlainOptions{Color: color})
	r.ProductTitle(plan.Title, "Review what Remork will do before it changes anything.")
	keys := make([]string, 0, len(plan.Target))
	for key := range plan.Target {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		r.KeyValue(key, plan.Target[key])
	}
	actions := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		actions = append(actions, action.Label)
	}
	r.ActionList("Actions", actions)
	if len(plan.Risks) > 0 {
		r.List("Risks", plan.Risks)
	}
	r.Next(plan.Next)
}

func executeSetupPrepareServerPlan(w interface{ Write([]byte) (int, error) }, color output.ColorMode, values map[string]string) error {
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		return err
	}
	spec.Confirmed = true
	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		return err
	}
	renderSetupPlan(w, color, plan)
	return nil
}

func runSetupPrepareServer(cmd *cobra.Command, opts Options) error {
	values, err := runTUIForm(cmd, "Prepare server", setupPrepareServerFields(nil))
	if err != nil {
		return err
	}
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		return err
	}
	spec.Confirmed = false
	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		return err
	}
	renderSetupPlan(cmd.OutOrStdout(), commandColorMode(cmd), plan)
	ok, err := prompt.Confirm(prompt.Options{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr()}, "execute setup plan?")
	if err != nil || !ok {
		return err
	}
	store, err := configStore(opts)
	if err != nil {
		return err
	}
	return runDaemonDeploy(cmd.OutOrStdout(), daemonDeployOptions{
		action:     spec.Action,
		hostName:   spec.HostName,
		sshTarget:  spec.SSHTarget,
		roots:      spec.Roots,
		addr:       spec.Addr,
		localBin:   spec.LocalBin,
		remoteBin:  spec.RemoteBin,
		tokenFile:  spec.TokenFile,
		url:        spec.URL,
		tokenEnv:   spec.TokenEnv,
		noProxy:    spec.NoProxy,
		verify:     spec.Verify,
		execute:    true,
		yes:        true,
		probe:      opts.DaemonProbe,
		version:    opts.Version,
		ctx:        cmd.Context(),
		color:      commandColorMode(cmd),
		canPrompt:  true,
		runner:     opts.CommandRunner,
		store:      store,
		storeReady: true,
	})
}

func runSetupConnectProject(cmd *cobra.Command, opts Options) error {
	localRoot, err := filepath.Abs(opts.WorkingDir)
	if err != nil {
		return err
	}
	values, err := runTUIForm(cmd, "Connect this project", []tui.Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab"},
		{Key: "remote_root", Label: "Remote workspace root", Placeholder: "/absolute/remote/workspace"},
		{Key: "first_sync", Label: "Run first sync y/N", Placeholder: "yes", Initial: "yes"},
	})
	if err != nil {
		return err
	}
	spec, firstSync, err := setupConnectProjectSpec(localRoot, values)
	if err != nil {
		return err
	}
	if err := ExecuteWorkspaceBindSpec(opts, spec); err != nil {
		return err
	}
	if firstSync {
		cmd.Root().SetArgs([]string{"sync"})
		return cmd.Root().ExecuteContext(cmd.Context())
	}
	plainRenderer(cmd, false).Next([]string{"remork sync"})
	return nil
}

func runSetupUpdateServer(cmd *cobra.Command, opts Options) error {
	_ = opts
	values, err := runTUIForm(cmd, "Update server", setupPrepareServerFields(nil))
	if err != nil {
		return err
	}
	spec, err := setupUpdateServerSpec(values)
	if err != nil {
		return err
	}
	spec.Confirmed = false
	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		return err
	}
	renderSetupPlan(cmd.OutOrStdout(), commandColorMode(cmd), plan)
	return nil
}

func runSetupRepair(cmd *cobra.Command, opts Options) error {
	_ = opts
	plainRenderer(cmd, false).ProductTitle("Repair setup", "Run remork doctor to inspect host, daemon, auth, roots, and workspace binding.")
	plainRenderer(cmd, false).Next([]string{"remork doctor"})
	return nil
}
