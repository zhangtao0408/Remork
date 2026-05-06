package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/output"
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
	return &cobra.Command{
		Use:   "status HOST",
		Short: "Show daemon version, platform, roots, threshold, and auth state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := configStore(opts)
			if err != nil {
				return err
			}
			cfg, err := store.Load()
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return codedCommandError{
						code: exitcode.InvalidUsageOrConfig,
						err:  fmt.Errorf("remork is not configured on this machine; run remork host add %s --url URL", args[0]),
						fix:  fmt.Sprintf("run remork host add %s --url URL", args[0]),
					}
				}
				return err
			}
			host, ok := cfg.Hosts[args[0]]
			if !ok {
				return fmt.Errorf("host %q is not configured; run remork host add %s --url URL", args[0], args[0])
			}
			token, err := auth.TokenFromEnv(host.TokenEnv)
			if err != nil {
				return tokenEnvCommandError(host, err)
			}
			clientID := cfg.ClientID
			if clientID == "" {
				clientID = "remork-cli"
			}
			status, err := client.NewWithOptions(client.Options{BaseURL: host.URL, ClientID: clientID, Token: token, NoProxy: host.NoProxy}).StatusContext(cmd.Context())
			if err != nil {
				return daemonStatusCommandError(host, err)
			}
			out := cmd.OutOrStdout()
			r := plainRenderer(cmd, false)
			r.Section("Daemon status")
			r.KeyValue("host", host.Name)
			r.KeyValue("url", host.URL)
			r.KeyValue("version", emptyAs(status.Version, "unknown"))
			r.KeyValue("platform", emptyAs(status.Platform, "unknown"))
			r.KeyValue("large_file_threshold", fmt.Sprintf("%d bytes", status.Threshold))
			r.KeyValue("watch_supported", status.WatchSupported)
			r.KeyValue("auth", daemonAuthState(host, token))
			fmt.Fprintln(out, "roots:")
			for _, root := range status.Roots {
				fmt.Fprintf(out, "  - %s\n", root)
			}
			if len(status.Roots) == 0 {
				fmt.Fprintln(out, "  - <none>")
			}
			return nil
		},
	}
}

func newDaemonDeployCommand(action string, opts Options) *cobra.Command {
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
			if len(args) == 0 {
				mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
				if !mode.Wizard {
					return fmt.Errorf("remork daemon %s requires HOST in non-interactive mode; run remork daemon %s from an interactive terminal or pass remork daemon %s HOST", action, action, action)
				}
				values, err := runTUIForm(cmd, "Daemon "+action, []tui.Field{
					{Key: "host", Label: "Host", Placeholder: "my-lab"},
				})
				if err != nil {
					return err
				}
				if strings.TrimSpace(values["host"]) == "" {
					return fmt.Errorf("HOST is required")
				}
				deploy.hostName = strings.TrimSpace(values["host"])
			} else {
				deploy.hostName = args[0]
			}
			host, hasHost, err := loadConfiguredHost(opts, deploy.hostName)
			if err != nil {
				return err
			}
			if hasHost && deploy.sshTarget == "" {
				deploy.sshTarget = sshTargetFromHost(host)
			}
			if len(deployAllowedRoots(deploy)) == 0 && action == "install" {
				mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
				if !mode.Wizard {
					return fmt.Errorf("--root is required for daemon install")
				}
				values, err := runTUIForm(cmd, "Daemon "+action, []tui.Field{
					{Key: "root", Label: "Allowed root", Placeholder: "/absolute/allowed/root"},
				})
				if err != nil {
					return err
				}
				deploy.roots = append(deploy.roots, strings.TrimSpace(values["root"]))
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
			deploy.storeReady = true
			deploy.ctx = cmd.Context()
			return runDaemonDeploy(cmd.OutOrStdout(), deploy)
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
	cmd.Flags().BoolVar(&deploy.execute, "execute", false, "Run generated scp and ssh deployment commands")
	cmd.Flags().BoolVar(&deploy.yes, "yes", false, "Confirm deployment command execution")
	cmd.Flags().BoolVar(&deploy.allowUnauthenticatedNetworkBind, "allow-unauthenticated-network-bind", false, "Allow remorkd to listen on a non-loopback address without --token-file")
	return cmd
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
	renderer := output.NewPlainRenderer(out, output.PlainOptions{})
	renderer.Section("Daemon " + deploy.action)
	remote := deploy.sshTarget
	if remote == "" {
		remote = deploy.hostName
	}
	startCmd := remoteStartCommand(deploy)
	fmt.Fprintf(out, "remorkd %s plan for %s\n\n", deploy.action, deploy.hostName)
	if insecureNoTokenNonLoopbackAddr(deploy.addr, deploy.tokenFile != "") {
		fmt.Fprintf(out, "%s this plan starts remorkd on a non-loopback or wildcard address without authentication.\n", output.Warning(out, "WARNING:"))
		fmt.Fprintln(out, "Anyone who can reach that address can use apply/file access and writes, remote command execution, and shell endpoints.")
		fmt.Fprintln(out, "Use --token-file for remorkd and configure the CLI with remork host add --token-env before using this on shared VPNs or multi-user networks.")
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "Run these commands from this machine. They copy a prebuilt daemon and start it without remote Go, npm, apt, brew, or internet.")
	fmt.Fprintf(out, "ssh %s %s\n", shellQuote(remote), shellQuote(remotePrepareCommand(deploy)))
	fmt.Fprintf(out, "scp %s %s:%s\n", shellQuote(deploy.localBin), shellQuote(remote), shellQuote(remoteSCPDestinationPath(deploy.remoteBin)))
	fmt.Fprintf(out, "ssh %s %s\n", shellQuote(remote), shellQuote(remoteChmodCommand(deploy.remoteBin)))
	if startCmd != "" {
		fmt.Fprintf(out, "ssh %s %s\n", shellQuote(remote), shellQuote(startCmd))
	}
	fmt.Fprintln(out)
	if deploy.url != "" {
		fmt.Fprintf(out, "Then configure the host URL:\n  %s\n", hostAddCommand(deploy))
	} else {
		placeholder := deploy
		placeholder.url = "http://HOST:" + daemonPort(deploy.addr)
		fmt.Fprintf(out, "Then configure the host URL if needed:\n  %s\n", hostAddCommand(placeholder))
	}
	fmt.Fprintf(out, "Verify:\n  remork daemon status %s\n", deploy.hostName)
}

func runDaemonDeploy(out interface{ Write([]byte) (int, error) }, deploy daemonDeployOptions) error {
	if err := validateDaemonDeployPlan(deploy); err != nil {
		return err
	}
	if deploy.execute {
		if err := validateDaemonDeployExecution(deploy); err != nil {
			return err
		}
		if !deploy.yes {
			return codedCommandError{
				code: exitcode.InvalidUsageOrConfig,
				err:  fmt.Errorf("--execute requires --yes"),
				fix:  "rerun with --yes after reviewing the install plan",
			}
		}
		if deploy.verify && deploy.url == "" && !deploy.storeReady {
			return codedCommandError{
				code: exitcode.InvalidUsageOrConfig,
				err:  fmt.Errorf("--verify requires --url or an existing configured host %q", deploy.hostName),
				fix:  "pass --url URL or run remork host add first",
			}
		}
	}
	printDaemonDeployPlan(out, deploy)
	if !deploy.execute {
		return nil
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
		printRemoteBinaryState(out, state, deploy.version, "before")
	}

	if err := runDeployStep(out, runner, "prepare remote directories", "ssh", remote, remotePrepareCommand(deploy)); err != nil {
		return err
	}
	if err := runDeployStep(out, runner, "stop existing remorkd daemon", "ssh", remote, remoteStopCommand()); err != nil {
		return err
	}
	if err := runDeployStep(out, runner, "copy remorkd binary", "scp", deploy.localBin, remote+":"+remoteSCPDestinationPath(deploy.remoteBin)); err != nil {
		return err
	}
	if err := runDeployStep(out, runner, "mark remorkd executable", "ssh", remote, remoteChmodCommand(deploy.remoteBin)); err != nil {
		return err
	}
	if want := expectedDaemonVersion(deploy.version); want != "" {
		state, err := remoteBinaryState(runner, remote, deploy)
		if err != nil {
			return fmt.Errorf("remote remorkd version check after copy failed on %s: %w", remote, err)
		}
		printRemoteBinaryState(out, state, deploy.version, "after")
		if !state.Installed || state.Version != want {
			got := state.Version
			if got == "" {
				got = "not installed"
			}
			return fmt.Errorf("remote remorkd version mismatch after copy: got %s, want %s", got, want)
		}
		output.NewPlainRenderer(out, output.PlainOptions{}).Success("copied remorkd version verified: " + want)
	}
	if startCmd := remoteStartCommand(deploy); startCmd != "" {
		if err := runDeployStep(out, runner, "start remorkd daemon", "ssh", remote, startCmd); err != nil {
			return err
		}
	}
	if deploy.url != "" {
		if err := saveDeployHost(deploy); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s host %s configured: %s\n", output.Success(out, "ok"), deploy.hostName, deploy.url)
	}
	if deploy.verify {
		ctx := deploy.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if err := verifyDeployHostReady(ctx, deploy); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s daemon status verified for %s\n", output.Success(out, "ok"), deploy.hostName)
	}
	fmt.Fprintf(out, "%s daemon deploy executed\n", output.Success(out, "ok"))
	return nil
}

func validateDaemonDeployPlan(deploy daemonDeployOptions) error {
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
	return nil
}

func runDeployStep(out interface{ Write([]byte) (int, error) }, runner commandRunner, label, name string, args ...string) error {
	renderer := output.NewPlainRenderer(out, output.PlainOptions{})
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

func printRemoteBinaryState(out interface{ Write([]byte) (int, error) }, state remoteBinaryInfo, expected, phase string) {
	if !state.Installed {
		if state.Path == "" {
			fmt.Fprintln(out, "remote binary: not installed")
			return
		}
		fmt.Fprintf(out, "remote binary: not installed at %s\n", state.Path)
		return
	}
	line := state.Line
	if line == "" {
		line = "installed but version is unknown"
	}
	if phase == "before" {
		if want := expectedDaemonVersion(expected); want != "" && state.Version != "" && state.Version != want {
			fmt.Fprintf(out, "%s remote binary: installed %s (will replace with %s)\n", output.Warning(out, "warn"), line, want)
			return
		}
	}
	fmt.Fprintf(out, "%s remote binary: installed %s\n", output.Success(out, "ok"), line)
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
