package cli

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/prompt"
	"remork/internal/tui"
	"remork/internal/workspace"
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
	sshTarget := strings.TrimSpace(values["ssh"])
	port := setupDaemonPort(values)
	daemonURL := strings.TrimSpace(values["url"])
	if daemonURL == "" {
		daemonURL = setupDaemonURL(sshTarget, host, port)
	}
	addr := strings.TrimSpace(values["addr"])
	if addr == "" {
		addr = "0.0.0.0:" + port
	}
	tokenEnv := strings.TrimSpace(values["token_env"])
	tokenFile := strings.TrimSpace(values["token_file"])
	if tokenFile == "" && tokenEnv != "" && insecureNoTokenNonLoopbackAddr(addr, false) {
		tokenFile = ".remork/remork.token"
	}
	allowUnauthenticated := false
	if raw := strings.TrimSpace(values["allow_unauthenticated_network_bind"]); raw != "" {
		allowUnauthenticated, err = parseDaemonDeployBool(raw, "allow unauthenticated network bind")
		if err != nil {
			return DaemonDeploySpec{}, HostConfigSpec{}, err
		}
	}
	spec := DaemonDeploySpec{
		Action:                          "install",
		HostName:                        host,
		SSHTarget:                       sshTarget,
		Roots:                           splitDaemonDeployRoots(values["roots"]),
		Addr:                            addr,
		LocalBin:                        strings.TrimSpace(values["local_bin"]),
		RemoteBin:                       strings.TrimSpace(values["remote_bin"]),
		TokenFile:                       tokenFile,
		URL:                             daemonURL,
		TokenEnv:                        tokenEnv,
		NoProxy:                         noProxy,
		Verify:                          verify,
		Execute:                         true,
		AllowUnauthenticatedNetworkBind: allowUnauthenticated,
	}
	return spec, HostConfigSpec{Name: host, URL: daemonURL, TokenEnv: spec.TokenEnv, NoProxy: noProxy}, nil
}

func setupPrepareServerFields(initial map[string]string) []tui.Field {
	if initial == nil {
		initial = map[string]string{}
	}
	return []tui.Field{
		{Section: "Server", Key: "host", Label: "Host", Placeholder: "my-lab", Initial: initial["host"], Help: "Saved Remork host name. Setup reuses the current workspace host when available."},
		{Section: "Server", Key: "ssh", Label: "SSH target", Placeholder: "user@server", Initial: initial["ssh"], Help: "How this Mac reaches the server over SSH for install or upgrade."},
		{Section: "Server", Key: "roots", Label: "Allowed roots", Placeholder: "/absolute/allowed/root", Initial: initial["roots"], Help: "Remote base directories remorkd is allowed to serve."},
		{Section: "Network", Key: "port", Label: "Port", Placeholder: "17731", Initial: firstNonEmpty(initial["port"], "17731"), Help: "Remork derives the daemon URL and listen address from this port."},
		{Section: "Auth", Key: "token_env", Label: "Token env", Initial: initial["token_env"], Help: "Optional. Use REMORK_TOKEN when you want token auth; leave empty to preserve a trusted private-network setup."},
		{Section: "Options", Key: "no_proxy", Label: "Bypass proxy y/N", Placeholder: "no", Initial: firstNonEmpty(initial["no_proxy"], "no"), Help: "Use yes for VPN or private IPs that should bypass local proxy variables."},
		{Section: "Options", Key: "verify", Label: "Verify y/N", Placeholder: "yes", Initial: firstNonEmpty(initial["verify"], "yes"), Help: "Run daemon status after setup to confirm the server is reachable."},
	}
}

func setupDaemonPort(values map[string]string) string {
	if port := strings.TrimSpace(values["port"]); port != "" {
		return strings.TrimPrefix(port, ":")
	}
	if port := portFromAddr(values["addr"]); port != "" {
		return port
	}
	if port := portFromURL(values["url"]); port != "" {
		return port
	}
	return "17731"
}

func setupDaemonURL(sshTarget, host, port string) string {
	target := firstNonEmpty(sshTarget, host, "localhost")
	if strings.Contains(target, "@") {
		target = target[strings.LastIndex(target, "@")+1:]
	}
	return "http://" + target + ":" + port
}

func portFromAddr(addr string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return ""
	}
	return port
}

func portFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if port := u.Port(); port != "" {
		return port
	}
	switch u.Scheme {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
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

func setupCurrentServerInitialValues(opts Options) map[string]string {
	initial := map[string]string{"verify": "yes"}
	binding, _, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return initial
	}
	store, err := configStore(opts)
	if err != nil {
		return initial
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return initial
	}
	host, ok := cfg.Hosts[binding.Host]
	if !ok {
		initial["host"] = binding.Host
		initial["roots"] = binding.RemoteRoot
		return initial
	}
	initial["host"] = host.Name
	initial["ssh"] = setupSSHTargetFromHostConfig(host, cfg.Hosts)
	initial["roots"] = binding.RemoteRoot
	initial["url"] = host.URL
	initial["port"] = firstNonEmpty(portFromURL(host.URL), "17731")
	initial["token_env"] = host.TokenEnv
	if host.TokenEnv == "" {
		initial["allow_unauthenticated_network_bind"] = "yes"
	}
	initial["no_proxy"] = yesNo(host.NoProxy)
	if opts.DaemonProbe != nil {
		if status, err := opts.DaemonProbe.Status(context.Background(), host, cfg.ClientID); err == nil && len(status.Roots) > 0 {
			initial["roots"] = strings.Join(status.Roots, ", ")
		}
	}
	return initial
}

func setupSSHTargetFromHost(host config.Host) string {
	parsed, err := url.Parse(host.URL)
	if err != nil {
		return host.Name
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return host.Name
	}
	if net.ParseIP(hostname) != nil && host.Name != "" {
		return host.Name
	}
	return hostname
}

func setupSSHTargetFromHostConfig(host config.Host, hosts map[string]config.Host) string {
	best := ""
	for name, candidate := range hosts {
		if name == host.Name || candidate.URL != host.URL {
			continue
		}
		if best == "" || len(name) < len(best) {
			best = name
		}
	}
	if best != "" {
		return best
	}
	return setupSSHTargetFromHost(host)
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
	spec.Execute = false
	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		return err
	}
	renderSetupPlan(w, color, plan)
	return nil
}

func runSetupPrepareServer(cmd *cobra.Command, opts Options) error {
	initial := map[string]string{}
	for {
		values, err := runTUIForm(cmd, "Prepare server", setupPrepareServerFields(initial))
		if err != nil {
			return err
		}
		err = runSetupDaemonPlanAndExecute(cmd, opts, values, "install")
		if err == nil {
			return nil
		}
		plainErrRenderer(cmd, false).Error(err.Error(), setupErrorFix(err))
		plainErrRenderer(cmd, false).Step("update the form values and submit again")
		initial = values
	}
}

func runSetupDaemonPlanAndExecute(cmd *cobra.Command, opts Options, values map[string]string, action string) error {
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		return err
	}
	spec.Action = action
	spec.Confirmed = false
	planSpec := spec
	planSpec.Execute = false
	plan, err := BuildDaemonDeployPlan(planSpec)
	if err != nil {
		return err
	}
	renderSetupPlan(cmd.OutOrStdout(), commandColorMode(cmd), plan)
	ok, err := prompt.Confirm(prompt.Options{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr()}, "execute setup plan?")
	if err != nil || !ok {
		return err
	}
	deploy := daemonDeployOptions{
		action:    spec.Action,
		hostName:  spec.HostName,
		sshTarget: spec.SSHTarget,
		roots:     spec.Roots,
		addr:      spec.Addr,
		localBin:  spec.LocalBin,
		remoteBin: spec.RemoteBin,
		tokenFile: spec.TokenFile,
		url:       spec.URL,
		tokenEnv:  spec.TokenEnv,
		noProxy:   spec.NoProxy,
		verify:    spec.Verify,
		execute:   true,
		yes:       true,
		probe:     opts.DaemonProbe,
		version:   opts.Version,
		ctx:       cmd.Context(),
		color:     commandColorMode(cmd),
		canPrompt: true,
		runner:    opts.CommandRunner,
	}
	return prepareAndRunDaemonDeploy(cmd, opts, deploy, false)
}

func runSetupConnectProject(cmd *cobra.Command, opts Options) error {
	localRoot, err := filepath.Abs(opts.WorkingDir)
	if err != nil {
		return err
	}
	values, err := runTUIForm(cmd, "Connect this project", []tui.Field{
		{Section: "Workspace", Key: "host", Label: "Host", Placeholder: "my-lab", Help: "Saved Remork host profile for the daemon serving this workspace."},
		{Section: "Workspace", Key: "remote_root", Label: "Remote workspace root", Placeholder: "/absolute/remote/workspace", Help: "Remote project directory to bind to this local folder."},
		{Section: "First run", Key: "first_sync", Label: "Run first sync y/N", Placeholder: "yes", Initial: "yes", Help: "Download current remote files after binding."},
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
	initial := setupCurrentServerInitialValues(opts)
	for {
		values, err := runTUIForm(cmd, "Update server", setupPrepareServerFields(initial))
		if err != nil {
			return err
		}
		err = runSetupDaemonPlanAndExecute(cmd, opts, values, "upgrade")
		if err == nil {
			return nil
		}
		plainErrRenderer(cmd, false).Error(err.Error(), setupErrorFix(err))
		plainErrRenderer(cmd, false).Step("update the form values and submit again")
		initial = values
	}
}

func runSetupRepair(cmd *cobra.Command, opts Options) error {
	_ = opts
	plainRenderer(cmd, false).ProductTitle("Repair setup", "Run remork doctor to inspect host, daemon, auth, roots, and workspace binding.")
	plainRenderer(cmd, false).Next([]string{"remork doctor"})
	return nil
}

func setupErrorFix(err error) string {
	if strings.Contains(err.Error(), "without authentication") {
		return "fill Token env with REMORK_TOKEN so setup uses the default remote token file, or press esc and use the advanced daemon command for unauthenticated private-network bind"
	}
	if fix := commandErrorFix(err); fix != "" {
		return fix
	}
	return "edit the highlighted setup fields, or press esc and use remork daemon --help for advanced options"
}
