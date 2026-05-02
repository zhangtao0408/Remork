package cli

import (
	"context"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/config"
	"remork/internal/safety"
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
				return err
			}
			host, ok := cfg.Hosts[args[0]]
			if !ok {
				return fmt.Errorf("host %q is not configured; run remork host add %s --url URL", args[0], args[0])
			}
			token, err := auth.TokenFromEnv(host.TokenEnv)
			if err != nil {
				return err
			}
			clientID := cfg.ClientID
			if clientID == "" {
				clientID = "remork-cli"
			}
			status, err := client.NewWithOptions(client.Options{BaseURL: host.URL, ClientID: clientID, Token: token, NoProxy: host.NoProxy}).StatusContext(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "host: %s\n", host.Name)
			fmt.Fprintf(out, "url: %s\n", host.URL)
			fmt.Fprintf(out, "version: %s\n", emptyAs(status.Version, "unknown"))
			fmt.Fprintf(out, "platform: %s\n", emptyAs(status.Platform, "unknown"))
			fmt.Fprintf(out, "large_file_threshold: %d bytes\n", status.Threshold)
			fmt.Fprintf(out, "watch_supported: %t\n", status.WatchSupported)
			fmt.Fprintf(out, "auth: %s\n", daemonAuthState(host, token))
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
	}
	cmd := &cobra.Command{
		Use:   action + " HOST --root /absolute/allowed/root [--root /another/root]",
		Short: action + " remorkd using an offline binary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploy.hostName = args[0]
			host, hasHost, err := loadConfiguredHost(opts, args[0])
			if err != nil {
				return err
			}
			if hasHost && deploy.sshTarget == "" {
				deploy.sshTarget = sshTargetFromHost(host)
			}
			if len(deployAllowedRoots(deploy)) == 0 && action == "install" {
				return fmt.Errorf("--root is required for daemon install")
			}
			for _, root := range deployAllowedRoots(deploy) {
				if strings.TrimSpace(root) == "" {
					return fmt.Errorf("--root cannot be empty")
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
			if deploy.verify && deploy.url == "" && !hasHost {
				return fmt.Errorf("--verify requires --url or an existing configured host %q", deploy.hostName)
			}
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
	return cmd
}

type daemonDeployOptions struct {
	action         string
	hostName       string
	sshTarget      string
	root           string
	roots          []string
	addr           string
	localBin       string
	remoteBin      string
	platform       string
	tokenFile      string
	url            string
	tokenEnv       string
	noProxy        bool
	verify         bool
	execute        bool
	yes            bool
	store          config.Store
	storeReady     bool
	probe          DaemonProbe
	ctx            context.Context
	verifyTimeout  time.Duration
	verifyInterval time.Duration
	runner         commandRunner
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
	remote := deploy.sshTarget
	if remote == "" {
		remote = deploy.hostName
	}
	startCmd := remoteStartCommand(deploy)
	fmt.Fprintf(out, "remorkd %s plan for %s\n\n", deploy.action, deploy.hostName)
	if insecureNoTokenNonLoopbackAddr(deploy.addr, deploy.tokenFile != "") {
		fmt.Fprintln(out, "WARNING: this plan starts remorkd on a non-loopback or wildcard address without authentication.")
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
		fmt.Fprintf(out, "Then configure the host URL if needed:\n  remork host add %s --url http://HOST:%s\n", deploy.hostName, daemonPort(deploy.addr))
	}
	fmt.Fprintf(out, "Verify:\n  remork daemon status %s\n", deploy.hostName)
}

func runDaemonDeploy(out interface{ Write([]byte) (int, error) }, deploy daemonDeployOptions) error {
	printDaemonDeployPlan(out, deploy)
	if !deploy.execute {
		return nil
	}
	if !deploy.yes {
		return fmt.Errorf("--execute requires --yes")
	}
	if deploy.verify && deploy.url == "" && !deploy.storeReady {
		return fmt.Errorf("--verify requires --url or an existing configured host %q", deploy.hostName)
	}
	runner := deploy.runner
	if runner == nil {
		runner = osCommandRunner{}
	}
	remote := deploy.sshTarget
	if remote == "" {
		remote = deploy.hostName
	}
	if err := runner.Run("ssh", remote, remotePrepareCommand(deploy)); err != nil {
		return err
	}
	if err := runner.Run("scp", deploy.localBin, remote+":"+remoteSCPDestinationPath(deploy.remoteBin)); err != nil {
		return err
	}
	if err := runner.Run("ssh", remote, remoteChmodCommand(deploy.remoteBin)); err != nil {
		return err
	}
	if startCmd := remoteStartCommand(deploy); startCmd != "" {
		if err := runner.Run("ssh", remote, startCmd); err != nil {
			return err
		}
	}
	if deploy.url != "" {
		if err := saveDeployHost(deploy); err != nil {
			return err
		}
		fmt.Fprintf(out, "host %s configured: %s\n", deploy.hostName, deploy.url)
	}
	if deploy.verify {
		ctx := deploy.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if err := verifyDeployHostReady(ctx, deploy); err != nil {
			return err
		}
		fmt.Fprintf(out, "daemon status verified for %s\n", deploy.hostName)
	}
	fmt.Fprintln(out, "daemon deploy executed")
	return nil
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
	stopCmd := "if [ -f " + remotePathShellArg(pidFile) + " ] && kill -0 \"$(cat " + remotePathShellArg(pidFile) + ")\" 2>/dev/null; then kill \"$(cat " + remotePathShellArg(pidFile) + ")\"; fi"
	startCmd := "nohup " + strings.Join(args, " ") + " </dev/null >" + remotePathShellArg(logFile) + " 2>&1 & echo $! >" + remotePathShellArg(pidFile)
	return stopCmd + "; " + startCmd
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
		return err
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
