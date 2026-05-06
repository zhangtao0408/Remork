package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	pathpkg "path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/prompt"
	"remork/internal/safety"
	"remork/internal/tui"
)

func addDaemonCommand(root *cobra.Command, opts Options) {
	daemon := &cobra.Command{
		Use:   "daemon",
		Short: "Install, upgrade, or inspect remorkd",
	}
	daemon.AddCommand(newDaemonStatusCommand(opts))
	daemon.AddCommand(newDaemonDeployCommand("install", opts))
	daemon.AddCommand(newDaemonDeployCommand("upgrade", opts))
	root.AddCommand(daemon)
}

func newDaemonStatusCommand(opts Options) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status HOST",
		Short: "Show daemon version, platform, allowed roots, threshold, and auth state",
		Args:  exactArgsJSON(1, &jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := configStore(opts)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			cfg, err := store.Load()
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					err = codedCommandError{
						code: exitcode.InvalidUsageOrConfig,
						err:  fmt.Errorf("remork is not configured on this machine; run remork host add %s --url URL", args[0]),
						fix:  fmt.Sprintf("run remork host add %s --url URL", args[0]),
					}
					if jsonOut {
						return writeJSONCommandError(cmd, err)
					}
					return err
				}
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			host, ok := cfg.Hosts[args[0]]
			if !ok {
				err := codedCommandError{
					code: exitcode.InvalidUsageOrConfig,
					err:  fmt.Errorf("host %q is not configured", args[0]),
					fix:  fmt.Sprintf("run remork host add %s --url URL", args[0]),
				}
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			token, err := auth.TokenFromEnv(host.TokenEnv)
			if err != nil {
				err = tokenEnvCommandError(host, err)
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			clientID := cfg.ClientID
			if clientID == "" {
				clientID = "remork-cli"
			}
			status, err := client.NewWithOptions(client.Options{BaseURL: host.URL, ClientID: clientID, Token: token, NoProxy: host.NoProxy}).StatusContext(cmd.Context())
			if err != nil {
				err = daemonStatusCommandError(host, err)
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), daemonStatusJSON{
					Host:               host.Name,
					URL:                host.URL,
					Reachable:          true,
					Version:            status.Version,
					Platform:           status.Platform,
					AllowedRoots:       append([]string(nil), status.Roots...),
					Auth:               daemonAuthState(host, token),
					LargeFileThreshold: status.Threshold,
					WatchSupported:     status.WatchSupported,
				})
			}
			r := plainRenderer(cmd, false)
			r.Section("Daemon status")
			r.KeyValue("host", host.Name)
			r.KeyValue("url", host.URL)
			r.KeyValue("version", emptyAs(status.Version, "unknown"))
			r.KeyValue("platform", emptyAs(status.Platform, "unknown"))
			r.KeyValue("large_file_threshold", fmt.Sprintf("%d bytes", status.Threshold))
			r.KeyValue("watch_supported", status.WatchSupported)
			r.KeyValue("auth", daemonAuthState(host, token))
			if len(status.Roots) == 0 {
				r.List("allowed roots", nil)
			} else {
				r.List("allowed roots", status.Roots)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	return cmd
}

type daemonStatusJSON struct {
	Host               string   `json:"host"`
	URL                string   `json:"url"`
	Reachable          bool     `json:"reachable"`
	Version            string   `json:"version"`
	Platform           string   `json:"platform"`
	AllowedRoots       []string `json:"allowed_roots"`
	Auth               string   `json:"auth"`
	LargeFileThreshold int64    `json:"large_file_threshold"`
	WatchSupported     bool     `json:"watch_supported"`
}

func newDaemonDeployCommand(action string, opts Options) *cobra.Command {
	var dryRun bool
	deploy := daemonDeployOptions{
		action:    action,
		addr:      "0.0.0.0:17731",
		remoteBin: ".local/bin/remorkd",
		probe:     opts.DaemonProbe,
		version:   opts.Version,
	}
	cmd := &cobra.Command{
		Use:   action + " [HOST] --root /absolute/allowed/root [--root /another/root]",
		Short: action + " remorkd using an offline binary",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				deploy.hostName = args[0]
			}
			mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
			if shouldRunDaemonDeployForm(mode, action, deploy, dryRun) {
				return runDaemonDeployForm(cmd, opts, action, deploy, dryRun)
			}
			return prepareAndRunDaemonDeploy(cmd, opts, deploy, dryRun)
		},
	}
	cmd.Flags().StringArrayVar(&deploy.roots, "root", nil, "Remote allowed base root for remorkd; repeat to serve multiple base roots")
	cmd.Flags().StringVar(&deploy.addr, "addr", deploy.addr, "Remote daemon listen address")
	cmd.Flags().StringVar(&deploy.sshTarget, "ssh", "", "SSH target such as user@host; defaults to host URL hostname when configured")
	cmd.Flags().StringVar(&deploy.localBin, "local-bin", "", "Local prebuilt remorkd binary")
	cmd.Flags().StringVar(&deploy.remoteBin, "remote-bin", deploy.remoteBin, "Remote remorkd path")
	cmd.Flags().StringVar(&deploy.platform, "platform", "", "Daemon platform suffix such as linux-arm64")
	cmd.Flags().StringVar(&deploy.tokenFile, "token-file", "", "Remote token file passed to remorkd")
	cmd.Flags().StringVar(&deploy.url, "url", "", "Daemon URL to write to local host config after successful install")
	cmd.Flags().StringVar(&deploy.tokenEnv, "token-env", "", "Environment variable containing the daemon token for local host config")
	cmd.Flags().BoolVar(&deploy.noProxy, "no-proxy", false, "Bypass proxies for this host in local host config")
	cmd.Flags().BoolVar(&deploy.verify, "verify", false, "Call daemon status after host config is available")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the deployment plan without running ssh or scp")
	cmd.Flags().BoolVar(&deploy.execute, "execute", false, "Deprecated compatibility flag; deployment executes unless --dry-run is set")
	cmd.Flags().BoolVarP(&deploy.yes, "yes", "y", false, "Confirm deployment command execution without prompting")
	cmd.Flags().BoolVar(&deploy.allowUnauthenticatedNetworkBind, "allow-unauthenticated-network-bind", false, "Allow remorkd to listen on a non-loopback address without --token-file")
	return cmd
}

func shouldRunDaemonDeployForm(mode interactionMode, action string, deploy daemonDeployOptions, dryRun bool) bool {
	if !mode.Wizard && !mode.RichOutput {
		return false
	}
	missingHost := strings.TrimSpace(deploy.hostName) == ""
	missingRoots := len(deployAllowedRoots(deploy)) == 0 && (action == "install" || (action == "upgrade" && !dryRun))
	if missingHost || missingRoots {
		return mode.Wizard
	}
	return mode.RichOutput && !dryRun && !deploy.yes
}

func runDaemonDeployForm(cmd *cobra.Command, opts Options, action string, deploy daemonDeployOptions, dryRun bool) error {
	for {
		values, err := runTUIForm(cmd, "Daemon "+action, daemonDeployFormFields(action, deploy, dryRun))
		if err != nil {
			return err
		}
		formDryRun, err := applyDaemonDeployFormValues(&deploy, values)
		if err != nil {
			plainErrRenderer(cmd, false).Error(err.Error(), "update the form value and submit again")
			continue
		}
		err = prepareAndRunDaemonDeploy(cmd, opts, deploy, formDryRun)
		if err == nil {
			return nil
		}
		if !daemonDeployFormRetryable(err) {
			return err
		}
		plainErrRenderer(cmd, false).Error(err.Error(), commandErrorFix(err))
		plainErrRenderer(cmd, false).Step("update the form values and submit again")
		dryRun = formDryRun
	}
}

func daemonDeployFormFields(action string, deploy daemonDeployOptions, dryRun bool) []tui.Field {
	_ = action
	return []tui.Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab", Initial: deploy.hostName},
		{Key: "roots", Label: "Allowed roots (--root, comma separated)", Placeholder: "/absolute/allowed/root", Initial: strings.Join(deployAllowedRoots(deploy), ", ")},
		{Key: "ssh", Label: "SSH target (--ssh)", Placeholder: "user@server or blank for host", Initial: deploy.sshTarget},
		{Key: "url", Label: "Daemon URL (--url)", Placeholder: "http://server:17731", Initial: deploy.url},
		{Key: "addr", Label: "Listen addr (--addr)", Placeholder: "0.0.0.0:17731", Initial: deploy.addr},
		{Key: "remote_bin", Label: "Remote binary (--remote-bin)", Placeholder: ".local/bin/remorkd", Initial: deploy.remoteBin},
		{Key: "local_bin", Label: "Local binary (--local-bin)", Placeholder: "auto", Initial: deploy.localBin},
		{Key: "platform", Label: "Platform (--platform)", Placeholder: "auto, linux-arm64, linux-amd64", Initial: deploy.platform},
		{Key: "token_file", Label: "Token file (--token-file)", Placeholder: ".remork/remork.token", Initial: deploy.tokenFile},
		{Key: "token_env", Label: "Token env (--token-env)", Placeholder: "REMORK_TOKEN", Initial: deploy.tokenEnv},
		{Key: "verify", Label: "Verify after install (--verify y/N)", Placeholder: "no", Initial: yesNo(deploy.verify)},
		{Key: "no_proxy", Label: "Bypass proxy (--no-proxy y/N)", Placeholder: "no", Initial: yesNo(deploy.noProxy)},
		{Key: "allow_unauthenticated_network_bind", Label: "Allow unauthenticated network bind (y/N)", Placeholder: "no", Initial: yesNo(deploy.allowUnauthenticatedNetworkBind)},
		{Key: "dry_run", Label: "Dry run only (--dry-run y/N)", Placeholder: "no", Initial: yesNo(dryRun)},
		{Key: "yes", Label: "Execute without final confirmation (-y/--yes y/N)", Placeholder: "no", Initial: yesNo(deploy.yes)},
	}
}

func applyDaemonDeployFormValues(deploy *daemonDeployOptions, values map[string]string) (bool, error) {
	deploy.hostName = strings.TrimSpace(values["host"])
	deploy.root = ""
	deploy.roots = splitDaemonDeployRoots(values["roots"])
	deploy.sshTarget = strings.TrimSpace(values["ssh"])
	deploy.url = strings.TrimSpace(values["url"])
	if addr := strings.TrimSpace(values["addr"]); addr != "" {
		deploy.addr = addr
	}
	if remoteBin := strings.TrimSpace(values["remote_bin"]); remoteBin != "" {
		deploy.remoteBin = remoteBin
	}
	deploy.localBin = strings.TrimSpace(values["local_bin"])
	deploy.platform = strings.TrimSpace(values["platform"])
	deploy.tokenFile = strings.TrimSpace(values["token_file"])
	deploy.tokenEnv = strings.TrimSpace(values["token_env"])

	var err error
	if deploy.verify, err = parseDaemonDeployBool(values["verify"], "verify"); err != nil {
		return false, err
	}
	if deploy.noProxy, err = parseDaemonDeployBool(values["no_proxy"], "no proxy"); err != nil {
		return false, err
	}
	if deploy.allowUnauthenticatedNetworkBind, err = parseDaemonDeployBool(values["allow_unauthenticated_network_bind"], "allow unauthenticated network bind"); err != nil {
		return false, err
	}
	dryRun, err := parseDaemonDeployBool(values["dry_run"], "dry run")
	if err != nil {
		return false, err
	}
	if deploy.yes, err = parseDaemonDeployBool(values["yes"], "yes"); err != nil {
		return false, err
	}
	return dryRun, nil
}

func splitDaemonDeployRoots(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	roots := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			roots = append(roots, part)
		}
	}
	return roots
}

func parseDaemonDeployBool(value, label string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "n", "no", "false", "0", "off":
		return false, nil
	case "y", "yes", "true", "1", "on":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be y/yes/true or n/no/false", label)
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func daemonDeployFormRetryable(err error) bool {
	code := commandErrorExitCode(err)
	return code == exitcode.InvalidUsageOrConfig || code == exitcode.NetworkUnavailable
}

func prepareAndRunDaemonDeploy(cmd *cobra.Command, opts Options, deploy daemonDeployOptions, dryRun bool) error {
	if strings.TrimSpace(deploy.hostName) == "" {
		return fmt.Errorf("remork daemon %s requires HOST in non-interactive mode; run remork daemon %s from an interactive terminal or pass remork daemon %s HOST", deploy.action, deploy.action, deploy.action)
	}
	host, hasHost, err := loadConfiguredHost(opts, deploy.hostName)
	if err != nil {
		return err
	}
	if hasHost && deploy.sshTarget == "" {
		deploy.sshTarget = sshTargetFromHost(host)
	}
	runner := opts.CommandRunner
	if runner == nil {
		runner = osCommandRunner{}
	}
	deploy.runner = runner
	if len(deployAllowedRoots(deploy)) == 0 && (deploy.action == "install" || (deploy.action == "upgrade" && !dryRun)) {
		if deploy.action == "install" {
			return fmt.Errorf("--root is required for daemon install")
		}
		return fmt.Errorf("--root is required for daemon upgrade when executing")
	}
	for _, root := range deployAllowedRoots(deploy) {
		if strings.TrimSpace(root) == "" {
			return fmt.Errorf("--root cannot be empty")
		}
	}
	if deploy.verify && deploy.url == "" && !hasHost {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("--verify requires --url or an existing configured host %q", deploy.hostName),
			fix:  "pass --url URL or run remork host add first",
		}
	}
	if deploy.localBin == "" {
		if deploy.platform == "" {
			plainErrRenderer(cmd, false).Step("detecting remote platform over SSH...")
			platform, err := detectRemoteDaemonPlatform(cmd.Context(), runner, deploySSHTarget(deploy))
			if err != nil {
				return err
			}
			deploy.platform = platform
			plainErrRenderer(cmd, false).Success("detected remote platform: " + deploy.platform)
		}
		localBin, err := resolveReleaseDaemonBinary(cmd.Context(), releaseBinaryOptions{
			Version:    opts.Version,
			HomeDir:    opts.HomeDir,
			Platform:   deploy.platform,
			LocalBin:   deploy.localBin,
			Downloader: defaultReleaseDownloader{},
		})
		if err != nil {
			return err
		}
		deploy.localBin = localBin
	}
	deploy.store, err = configStore(opts)
	if err != nil {
		return err
	}
	mode := commandInteractionMode(cmd, interactionRequest{})
	deploy.storeReady = true
	deploy.ctx = cmd.Context()
	deploy.color = commandColorMode(cmd)
	deploy.canPrompt = mode.RichOutput
	deploy.confirmIn = cmd.InOrStdin()
	deploy.confirmOut = cmd.ErrOrStderr()
	configureDaemonDeployExecution(&deploy, dryRun)
	return runDaemonDeploy(cmd.OutOrStdout(), deploy)
}

func configureDaemonDeployExecution(deploy *daemonDeployOptions, dryRun bool) {
	if dryRun {
		deploy.execute = false
		deploy.yes = false
		deploy.dryRun = true
		return
	}
	deploy.execute = true
	deploy.dryRun = false
}

type daemonDeployOptions struct {
	action                          string
	hostName                        string
	sshTarget                       string
	root                            string
	roots                           []string
	addr                            string
	localBin                        string
	remoteBin                       string
	platform                        string
	tokenFile                       string
	url                             string
	tokenEnv                        string
	noProxy                         bool
	verify                          bool
	dryRun                          bool
	execute                         bool
	yes                             bool
	allowUnauthenticatedNetworkBind bool
	store                           config.Store
	storeReady                      bool
	probe                           DaemonProbe
	ctx                             context.Context
	verifyTimeout                   time.Duration
	verifyInterval                  time.Duration
	runner                          commandRunner
	version                         string
	color                           output.ColorMode
	canPrompt                       bool
	confirmIn                       io.Reader
	confirmOut                      io.Writer
}

func loadConfiguredHost(opts Options, name string) (config.Host, bool, error) {
	store, err := configStore(opts)
	if err != nil {
		return config.Host{}, false, err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return config.Host{}, false, err
	}
	host, ok := cfg.Hosts[name]
	return host, ok, nil
}

func printDaemonDeployPlan(out interface{ Write([]byte) (int, error) }, deploy daemonDeployOptions) {
	renderer := output.NewPlainRenderer(out, output.PlainOptions{Color: deploy.color})
	renderer.Section("Daemon " + deploy.action)
	remote := deploy.sshTarget
	if remote == "" {
		remote = deploy.hostName
	}
	startCmd := remoteStartCommand(deploy)
	renderer.KeyValue("host", deploy.hostName)
	renderer.KeyValue("remote", remote)
	renderer.KeyValue("remote_bin", remoteCommandPath(deploy.remoteBin))
	if deploy.execute {
		if deploy.yes {
			renderer.KeyValue("mode", "execute confirmed")
		} else {
			renderer.KeyValue("mode", "execute after confirmation")
		}
	} else {
		renderer.KeyValue("mode", "dry run preview")
		renderer.Warning("No remote commands were executed.")
	}
	if insecureNoTokenNonLoopbackAddr(deploy.addr, deploy.tokenFile != "") {
		renderer.Warning("WARNING: this plan starts remorkd on a non-loopback or wildcard address without authentication.")
		renderer.List("Risk", []string{
			"Anyone who can reach that address can use apply/file access and writes, remote command execution, and shell endpoints.",
			"Use --token-file for remorkd and configure the CLI with remork host add --token-env before using this on shared VPNs or multi-user networks.",
		})
	}
	plan := []string{"Copy a prebuilt daemon from this machine."}
	if startCmd != "" {
		plan = append(plan, "Start remorkd without remote Go, npm, apt, brew, or internet.")
	} else {
		plan = append(plan, "Do not start remorkd because no allowed root is part of this plan.")
	}
	renderer.List("Plan", plan)
	renderer.Command("ssh " + shellQuote(remote) + " " + shellQuote(remotePrepareCommand(deploy)))
	renderer.Command("scp " + shellQuote(deploy.localBin) + " " + shellQuote(remote) + ":" + shellQuote(remoteSCPDestinationPath(deploy.remoteBin)))
	renderer.Command("ssh " + shellQuote(remote) + " " + shellQuote(remoteChmodCommand(deploy.remoteBin)))
	if startCmd != "" {
		renderer.Command("ssh " + shellQuote(remote) + " " + shellQuote(startCmd))
	}
	if deploy.url != "" {
		renderer.List("Then configure the host URL:", []string{hostAddCommand(deploy)})
	} else {
		placeholder := deploy
		placeholder.url = "http://HOST:" + daemonPort(deploy.addr)
		renderer.List("Then configure the host URL if needed:", []string{hostAddCommand(placeholder)})
	}
	renderer.List("Verify:", []string{"remork daemon status " + deploy.hostName})
	if !deploy.execute {
		renderer.List("Next:", []string{
			"review the commands above",
			"rerun without --dry-run and confirm, or pass -y/--yes to execute",
		})
		renderer.Success("preview generated; remote daemon was not changed")
	}
}

func runDaemonDeploy(out interface{ Write([]byte) (int, error) }, deploy daemonDeployOptions) error {
	if deploy.dryRun {
		deploy.execute = false
		deploy.yes = false
	}
	if err := validateDaemonDeployPlan(deploy); err != nil {
		return err
	}
	if deploy.execute {
		if deploy.verify && deploy.url == "" && !deploy.storeReady {
			return codedCommandError{
				code: exitcode.InvalidUsageOrConfig,
				err:  fmt.Errorf("--verify requires --url or an existing configured host %q", deploy.hostName),
				fix:  "pass --url URL or run remork host add first",
			}
		}
		if err := validateDaemonDeployExecution(deploy); err != nil {
			return err
		}
	} else {
		if err := validateLocalDaemonBinary(deploy.localBin); err != nil {
			return err
		}
	}
	if deploy.execute && !deploy.yes && !deploy.canPrompt {
		return daemonDeployRequiresConfirmationError(deploy.action)
	}
	printDaemonDeployPlan(out, deploy)
	if !deploy.execute {
		return nil
	}
	if !deploy.yes {
		ok, err := prompt.Confirm(prompt.Options{In: deploy.confirmIn, Out: deploy.confirmOut}, fmt.Sprintf("execute daemon %s on %s?", deploy.action, deploy.hostName))
		if err != nil {
			return err
		}
		if !ok {
			output.NewPlainRenderer(out, output.PlainOptions{Color: deploy.color}).Warning("daemon " + deploy.action + " cancelled")
			return nil
		}
	}
	runner := deploy.runner
	if runner == nil {
		runner = osCommandRunner{}
	}
	remote := deploy.sshTarget
	if remote == "" {
		remote = deploy.hostName
	}

	if deploy.version != "" {
		state, err := remoteBinaryState(runner, remote, deploy)
		if err != nil {
			return fmt.Errorf("remote remorkd preflight failed on %s: %w", remote, err)
		}
		printRemoteBinaryState(out, state, deploy.version, "before", deploy.color)
	}

	if err := runDeployStep(out, runner, deploy.color, "prepare remote directories", "ssh", remote, remotePrepareCommand(deploy)); err != nil {
		return err
	}
	if err := runDeployStep(out, runner, deploy.color, "stop existing remorkd daemon", "ssh", remote, remoteStopCommand()); err != nil {
		return err
	}
	if err := runDeployStep(out, runner, deploy.color, "copy remorkd binary", "scp", deploy.localBin, remote+":"+remoteSCPDestinationPath(deploy.remoteBin)); err != nil {
		return err
	}
	if err := runDeployStep(out, runner, deploy.color, "mark remorkd executable", "ssh", remote, remoteChmodCommand(deploy.remoteBin)); err != nil {
		return err
	}
	if want := expectedDaemonVersion(deploy.version); want != "" {
		state, err := remoteBinaryState(runner, remote, deploy)
		if err != nil {
			return fmt.Errorf("remote remorkd version check after copy failed on %s: %w", remote, err)
		}
		printRemoteBinaryState(out, state, deploy.version, "after", deploy.color)
		if !state.Installed || state.Version != want {
			got := state.Version
			if got == "" {
				got = "not installed"
			}
			return fmt.Errorf("remote remorkd version mismatch after copy: got %s, want %s", got, want)
		}
		output.NewPlainRenderer(out, output.PlainOptions{Color: deploy.color}).Success("copied remorkd version verified: " + want)
	}
	if startCmd := remoteStartCommand(deploy); startCmd != "" {
		if err := runDeployStep(out, runner, deploy.color, "start remorkd daemon", "ssh", remote, startCmd); err != nil {
			return err
		}
	}
	if deploy.url != "" {
		if err := saveDeployHost(deploy); err != nil {
			return err
		}
		output.NewPlainRenderer(out, output.PlainOptions{Color: deploy.color}).Success(fmt.Sprintf("host %s configured: %s", deploy.hostName, deploy.url))
	}
	if deploy.verify {
		ctx := deploy.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if err := verifyDeployHostReady(ctx, deploy); err != nil {
			return err
		}
		output.NewPlainRenderer(out, output.PlainOptions{Color: deploy.color}).Success("daemon status verified for " + deploy.hostName)
	}
	output.NewPlainRenderer(out, output.PlainOptions{Color: deploy.color}).Success("daemon deploy executed")
	return nil
}

func daemonDeployRequiresConfirmationError(action string) error {
	return codedCommandError{
		code: exitcode.PromptRequired,
		err:  fmt.Errorf("daemon %s requires confirmation", action),
		fix:  "run from an interactive terminal, pass -y/--yes to execute, or pass --dry-run to preview without changing the server",
	}
}

func validateDaemonDeployPlan(deploy daemonDeployOptions) error {
	if err := validateDaemonAddr(deploy.addr); err != nil {
		return err
	}
	if deploy.url != "" {
		if err := validateDaemonURL(deploy.url); err != nil {
			return codedCommandError{
				code: exitcode.InvalidUsageOrConfig,
				err:  err,
				fix:  "pass an http:// or https:// daemon URL",
			}
		}
	}
	for _, root := range deployAllowedRoots(deploy) {
		if !pathpkg.IsAbs(root) {
			return codedCommandError{
				code: exitcode.InvalidUsageOrConfig,
				err:  fmt.Errorf("--root must be an absolute remote path: %s", root),
				fix:  "pass a remote absolute path such as --root /home/me/project",
			}
		}
	}
	if deploy.execute && deploy.action == "upgrade" && len(deployAllowedRoots(deploy)) == 0 {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("daemon upgrade requires at least one --root when --execute is used"),
			fix:  "pass every root remorkd should continue serving, for example --root /home/me/project",
		}
	}
	if deploy.tokenFile != "" && deploy.tokenEnv == "" {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("--token-env is required with --token-file so the local CLI can authenticate to the protected daemon"),
			fix:  "set an environment variable with the daemon token and pass --token-env NAME",
		}
	}
	return nil
}

func validateDaemonDeployExecution(deploy daemonDeployOptions) error {
	if insecureNoTokenNonLoopbackAddr(deploy.addr, deploy.tokenFile != "") && !deploy.allowUnauthenticatedNetworkBind {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("refusing to execute remorkd on %s without authentication; pass --token-file, bind to 127.0.0.1, or add --allow-unauthenticated-network-bind for a trusted private network", deploy.addr),
			fix:  "pass --token-file with --token-env, bind to 127.0.0.1, or explicitly pass --allow-unauthenticated-network-bind",
		}
	}
	if err := validateLocalDaemonBinary(deploy.localBin); err != nil {
		return err
	}
	return nil
}

func validateDaemonAddr(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return invalidDaemonAddrError(addr, fmt.Errorf("listen address is empty"))
	}
	_, portValue, err := net.SplitHostPort(addr)
	if err != nil {
		return invalidDaemonAddrError(addr, err)
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		return invalidDaemonAddrError(addr, fmt.Errorf("port must be an integer from 1 to 65535"))
	}
	return nil
}

func invalidDaemonAddrError(addr string, err error) error {
	return codedCommandError{
		code: exitcode.InvalidUsageOrConfig,
		err:  fmt.Errorf("invalid daemon listen address %q: %w", addr, err),
		fix:  "pass --addr HOST:PORT, for example --addr 127.0.0.1:17731 or --addr :17731",
	}
}

func validateLocalDaemonBinary(localBin string) error {
	if strings.TrimSpace(localBin) == "" {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("local remorkd binary is required for --execute"),
			fix:  "pass --local-bin /path/to/remorkd or use a released remork version that can resolve the daemon binary",
		}
	}
	info, err := os.Stat(localBin)
	if err != nil {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("local remorkd binary %q is not available: %w", localBin, err),
			fix:  "build or download remorkd, then rerun with --local-bin /path/to/remorkd",
		}
	}
	if !info.Mode().IsRegular() {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("local remorkd binary %q is not a regular file", localBin),
			fix:  "pass --local-bin /path/to/remorkd",
		}
	}
	if info.Mode()&0o111 == 0 {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("local remorkd binary %q is not executable", localBin),
			fix:  "run chmod 0755 /path/to/remorkd, then rerun remork daemon install",
		}
	}
	f, err := os.Open(localBin)
	if err != nil {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("local remorkd binary %q is not readable: %w", localBin, err),
			fix:  "check file permissions, then rerun remork daemon install",
		}
	}
	_ = f.Close()
	return nil
}

func deploySSHTarget(deploy daemonDeployOptions) string {
	if deploy.sshTarget != "" {
		return deploy.sshTarget
	}
	return deploy.hostName
}

func detectRemoteDaemonPlatform(ctx context.Context, runner commandRunner, sshTarget string) (string, error) {
	_ = ctx
	sshTarget = strings.TrimSpace(sshTarget)
	if sshTarget == "" {
		return "", codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("remote SSH target is required to auto-detect daemon platform"),
			fix:  "pass HOST or --ssh SSH_TARGET, or pass --platform linux-arm64 or --platform linux-amd64",
		}
	}
	if runner == nil {
		runner = osCommandRunner{}
	}
	out, err := runner.Output("ssh", sshTarget, "uname -s; uname -m")
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return "", codedCommandError{
			code: exitcode.NetworkUnavailable,
			err:  fmt.Errorf("failed to auto-detect remote daemon platform via ssh %s: %w", sshTarget, err),
			fix:  "check SSH connectivity, pass --ssh SSH_TARGET, or pass --platform linux-arm64 or --platform linux-amd64",
		}
	}
	platform, err := parseRemoteDaemonPlatform(string(out))
	if err != nil {
		return "", err
	}
	return platform, nil
}

func parseRemoteDaemonPlatform(out string) (string, error) {
	fields := strings.Fields(strings.ToLower(out))
	if len(fields) < 2 {
		return "", codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("could not parse remote platform from uname output %q", strings.TrimSpace(out)),
			fix:  "pass --platform linux-arm64 or --platform linux-amd64",
		}
	}
	osName := fields[0]
	arch := fields[len(fields)-1]
	if osName != "linux" {
		return "", codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("unsupported remote daemon OS %q; only linux daemon releases are supported", osName),
			fix:  "install remorkd manually with --local-bin for this server, or use a linux server",
		}
	}
	switch arch {
	case "aarch64", "arm64":
		return "linux-arm64", nil
	case "x86_64", "amd64":
		return "linux-amd64", nil
	default:
		return "", codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("unsupported remote daemon architecture %q", arch),
			fix:  "supported release platforms are linux-arm64 and linux-amd64; pass --local-bin for a custom daemon binary",
		}
	}
}

func runDeployStep(out interface{ Write([]byte) (int, error) }, runner commandRunner, color output.ColorMode, label, name string, args ...string) error {
	renderer := output.NewPlainRenderer(out, output.PlainOptions{Color: color})
	renderer.Step(label + "...")
	if err := runner.Run(name, args...); err != nil {
		return fmt.Errorf("%s failed: %w", label, err)
	}
	renderer.Success(label)
	return nil
}

type remoteBinaryInfo struct {
	Installed bool
	Path      string
	Line      string
	Version   string
}

func remoteBinaryState(runner commandRunner, remote string, deploy daemonDeployOptions) (remoteBinaryInfo, error) {
	out, err := runner.Output("ssh", remote, remoteBinaryProbeCommand(deploy.remoteBin))
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return remoteBinaryInfo{}, fmt.Errorf("%w: %s", err, msg)
		}
		return remoteBinaryInfo{}, err
	}
	return parseRemoteBinaryProbeOutput(string(out)), nil
}

func remoteBinaryProbeCommand(remoteBin string) string {
	path := remotePathShellArg(remoteCommandPath(remoteBin))
	return "if [ -x " + path + " ]; then printf 'installed\\t'; " + path + " --version; else printf 'missing\\t%s\\n' " + path + "; fi"
}

func parseRemoteBinaryProbeOutput(out string) remoteBinaryInfo {
	line := strings.TrimSpace(out)
	info := remoteBinaryInfo{Line: line}
	if rest, ok := strings.CutPrefix(line, "installed\t"); ok {
		info.Installed = true
		info.Line = strings.TrimSpace(rest)
		info.Version = parseRemorkdVersion(rest)
		return info
	}
	if version := parseRemorkdVersion(line); version != "" {
		info.Installed = true
		info.Version = version
		return info
	}
	if rest, ok := strings.CutPrefix(line, "missing\t"); ok {
		info.Path = strings.TrimSpace(rest)
	}
	return info
}

func parseRemorkdVersion(line string) string {
	line = strings.TrimSpace(line)
	if rest, ok := strings.CutPrefix(line, "remorkd "); ok {
		return strings.TrimSpace(rest)
	}
	return ""
}

func printRemoteBinaryState(out interface{ Write([]byte) (int, error) }, state remoteBinaryInfo, expected, phase string, color output.ColorMode) {
	r := output.NewPlainRenderer(out, output.PlainOptions{Color: color})
	if !state.Installed {
		if state.Path == "" {
			r.Warning("remote binary: not installed")
			return
		}
		r.Warning("remote binary: not installed at " + state.Path)
		return
	}
	line := state.Line
	if line == "" {
		line = "installed but version is unknown"
	}
	if phase == "before" {
		if want := expectedDaemonVersion(expected); want != "" && state.Version != "" && state.Version != want {
			r.Warning(fmt.Sprintf("remote binary: installed %s (will replace with %s)", line, want))
			return
		}
	}
	r.Success("remote binary: installed " + line)
}

func expectedDaemonVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" {
		return ""
	}
	return version
}

func remotePrepareCommand(deploy daemonDeployOptions) string {
	dirs := []string{remoteDir(remoteCommandPath(deploy.remoteBin)), "$HOME/.remork/run", "$HOME/.remork/log"}
	quoted := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		quoted = append(quoted, remotePathShellArg(dir))
	}
	return "mkdir -p " + strings.Join(quoted, " ")
}

func remoteChmodCommand(remoteBin string) string {
	return "chmod 0755 " + remotePathShellArg(remoteCommandPath(remoteBin))
}

func remoteStopCommand() string {
	pidFile := "$HOME/.remork/run/remorkd.pid"
	quotedPidFile := remotePathShellArg(pidFile)
	return "if [ -f " + quotedPidFile + " ]; then pid=\"$(cat " + quotedPidFile + ")\"; if kill -0 \"$pid\" 2>/dev/null; then kill \"$pid\"; for i in 1 2 3 4 5; do kill -0 \"$pid\" 2>/dev/null || break; sleep 1; done; fi; rm -f " + quotedPidFile + "; fi"
}

func insecureNoTokenNonLoopbackAddr(addr string, hasToken bool) bool {
	return safety.UnsafeNoTokenNonLoopbackBind(addr, hasToken)
}

func remoteStartCommand(deploy daemonDeployOptions) string {
	roots := deployAllowedRoots(deploy)
	if len(roots) == 0 {
		return ""
	}
	remoteBin := remoteCommandPath(deploy.remoteBin)
	pidFile := "$HOME/.remork/run/remorkd.pid"
	logFile := "$HOME/.remork/log/remorkd.log"
	args := []string{remotePathShellArg(remoteBin)}
	for _, root := range roots {
		args = append(args, shellQuote("--root"), shellQuote(root))
	}
	args = append(args, shellQuote("--addr"), shellQuote(deploy.addr))
	if deploy.tokenFile != "" {
		args = append(args, shellQuote("--token-file"), shellQuote(deploy.tokenFile))
	}
	startCmd := "nohup " + strings.Join(args, " ") + " </dev/null >" + remotePathShellArg(logFile) + " 2>&1 & echo $! >" + remotePathShellArg(pidFile)
	return remoteStopCommand() + "; " + startCmd
}

func filepathForDaemonBinary(platform string) string {
	if platform == "" {
		platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	return "dist/remorkd-" + platform
}

func remoteCommandPath(path string) string {
	if path == "" {
		return "$HOME/.local/bin/remorkd"
	}
	if strings.HasPrefix(path, "~/") {
		return "$HOME/" + strings.TrimPrefix(path, "~/")
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "$HOME/") {
		return path
	}
	return "$HOME/" + strings.TrimPrefix(path, "./")
}

func remoteSCPDestinationPath(path string) string {
	if path == "" {
		return ".local/bin/remorkd"
	}
	if strings.HasPrefix(path, "$HOME/") {
		return "~/" + strings.TrimPrefix(path, "$HOME/")
	}
	if strings.HasPrefix(path, "~/") {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return strings.TrimPrefix(path, "./")
}

func remoteDir(path string) string {
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		return path[:idx]
	}
	return "."
}

func remotePathShellArg(path string) string {
	if strings.HasPrefix(path, "$HOME/") {
		tail := strings.TrimPrefix(path, "$HOME/")
		replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
		return `"$HOME/` + replacer.Replace(tail) + `"`
	}
	return shellQuote(path)
}

func hostAddCommand(deploy daemonDeployOptions) string {
	args := []string{"remork", "host", "add", deploy.hostName, "--url", deploy.url}
	if deploy.tokenEnv != "" {
		args = append(args, "--token-env", deploy.tokenEnv)
	}
	if deploy.noProxy {
		args = append(args, "--no-proxy")
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func saveDeployHost(deploy daemonDeployOptions) error {
	store := deploy.store
	if !deploy.storeReady {
		return fmt.Errorf("home directory is required for remork config")
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	cfg.Hosts[deploy.hostName] = config.Host{Name: deploy.hostName, URL: deploy.url, TokenEnv: deploy.tokenEnv, NoProxy: deploy.noProxy}
	return store.Save(cfg)
}

func verifyDeployHost(ctx context.Context, deploy daemonDeployOptions) error {
	store := deploy.store
	if !deploy.storeReady {
		return fmt.Errorf("--verify requires --url or an existing configured host %q", deploy.hostName)
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	host, ok := cfg.Hosts[deploy.hostName]
	if !ok {
		return fmt.Errorf("--verify requires --url or an existing configured host %q", deploy.hostName)
	}
	probe := deploy.probe
	if probe == nil {
		probe = httpDaemonProbe{}
	}
	clientID := cfg.ClientID
	if clientID == "" {
		clientID = "remork-cli"
	}
	status, err := probe.Status(ctx, host, clientID)
	if err != nil {
		return fmt.Errorf("status check failed for %s: %s", host.URL, explainDaemonStatusError(err))
	}
	if want := expectedDaemonVersion(deploy.version); want != "" {
		if status.Version == "" {
			return fmt.Errorf("daemon version mismatch at %s: got unknown, want %s", host.URL, want)
		}
		if status.Version != want {
			return fmt.Errorf("daemon version mismatch at %s: got %s, want %s", host.URL, status.Version, want)
		}
	}
	for _, root := range deployAllowedRoots(deploy) {
		ok, err := remoteRootAdvertised(status.Roots, root)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("daemon status verified but root %q is not advertised by host %q", root, deploy.hostName)
		}
	}
	return nil
}

func explainDaemonStatusError(err error) string {
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case 401, 403:
			return fmt.Sprintf("auth failed with HTTP %d; check --token-file and --token-env", httpErr.StatusCode)
		case 404:
			return "daemon URL is reachable but /status was not found; check the URL and port"
		default:
			body := strings.TrimSpace(httpErr.Body)
			if body == "" {
				body = "empty response body"
			}
			return fmt.Sprintf("daemon returned HTTP %d: %s", httpErr.StatusCode, body)
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "connection timed out; check VPN reachability, host, firewall, and daemon port"
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "connection refused"):
		return "connection refused; remorkd is not listening on that host/port or the port is wrong"
	case strings.Contains(lower, "no such host"):
		return "host name could not be resolved; check the daemon URL host"
	case strings.Contains(lower, "i/o timeout"):
		return "connection timed out; check VPN reachability, host, firewall, and daemon port"
	default:
		return msg
	}
}

func daemonStatusCommandError(host config.Host, err error) error {
	return daemonReachabilityCommandError(err, daemonStatusErrorFix(host, err))
}

func daemonReachabilityCommandError(err error, fix string) error {
	code := exitcode.NetworkUnavailable
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 401 || httpErr.StatusCode == 403 {
			code = exitcode.PermissionDenied
		}
	}
	return codedCommandError{
		code: code,
		err:  errors.New(explainDaemonStatusError(err)),
		fix:  fix,
	}
}

func daemonStatusErrorFix(host config.Host, err error) string {
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case 401, 403:
			if host.TokenEnv != "" {
				return "export " + host.TokenEnv + "=<token>, or update the host with remork host add " + host.Name + " --url " + host.URL + " --token-env TOKEN_ENV"
			}
			return "configure a token with remork host add " + host.Name + " --url " + host.URL + " --token-env TOKEN_ENV, or restart remorkd without auth only on trusted private networks"
		case 404:
			return "check the daemon URL path and port with remork host add " + host.Name + " --url URL"
		}
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "i/o timeout"), strings.Contains(lower, "no such host"):
		return "start remorkd, check VPN/firewall reachability, then verify the host URL with remork host add " + host.Name + " --url URL"
	default:
		return "run remork doctor for a full workspace readiness check"
	}
}

func tokenEnvCommandError(host config.Host, err error) error {
	fix := "export " + host.TokenEnv + "=<token>, or update the host with remork host add " + host.Name + " --url " + host.URL + " --token-env TOKEN_ENV"
	return codedCommandError{
		code: exitcode.PermissionDenied,
		err:  err,
		fix:  fix,
	}
}

func deployAllowedRoots(deploy daemonDeployOptions) []string {
	roots := make([]string, 0, len(deploy.roots)+1)
	if deploy.root != "" {
		roots = append(roots, deploy.root)
	}
	roots = append(roots, deploy.roots...)
	return roots
}

func verifyDeployHostReady(ctx context.Context, deploy daemonDeployOptions) error {
	timeout := deploy.verifyTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	interval := deploy.verifyInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = verifyDeployHost(ctx, deploy)
		if lastErr == nil {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("daemon verify did not become ready within %s: %w", timeout, lastErr)
		}
		sleep := interval
		if remaining < sleep {
			sleep = remaining
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return fmt.Errorf("daemon verify canceled before ready: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func sshTargetFromHost(host config.Host) string {
	u, err := url.Parse(host.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func daemonAuthState(host config.Host, token string) string {
	if host.TokenEnv == "" {
		return "no token configured"
	}
	if token == "" {
		return "token env " + host.TokenEnv + " is empty"
	}
	return "token env " + host.TokenEnv + " is set"
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func daemonPort(addr string) string {
	if strings.Contains(addr, ":") {
		parts := strings.Split(addr, ":")
		return parts[len(parts)-1]
	}
	return addr
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
