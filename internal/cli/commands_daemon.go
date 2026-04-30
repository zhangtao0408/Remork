package cli

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/config"
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
			status, err := client.NewWithOptions(client.Options{BaseURL: host.URL, ClientID: clientID, Token: token}).Status()
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
		remoteBin: "/tmp/remorkd",
	}
	cmd := &cobra.Command{
		Use:   action + " HOST --root /absolute/remote/root",
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
			if deploy.root == "" && action == "install" {
				return fmt.Errorf("--root is required for daemon install")
			}
			if deploy.localBin == "" {
				deploy.localBin = filepathForDaemonBinary(deploy.platform)
			}
			printDaemonDeployPlan(cmd.OutOrStdout(), deploy)
			return nil
		},
	}
	cmd.Flags().StringVar(&deploy.root, "root", "", "Remote workspace root for remorkd")
	cmd.Flags().StringVar(&deploy.addr, "addr", deploy.addr, "Remote daemon listen address")
	cmd.Flags().StringVar(&deploy.sshTarget, "ssh", "", "SSH target such as user@host; defaults to host URL hostname when configured")
	cmd.Flags().StringVar(&deploy.localBin, "local-bin", "", "Local prebuilt remorkd binary")
	cmd.Flags().StringVar(&deploy.remoteBin, "remote-bin", deploy.remoteBin, "Remote remorkd path")
	cmd.Flags().StringVar(&deploy.platform, "platform", "", "Daemon platform suffix such as linux-arm64")
	cmd.Flags().StringVar(&deploy.tokenFile, "token-file", "", "Remote token file passed to remorkd")
	return cmd
}

type daemonDeployOptions struct {
	action    string
	hostName  string
	sshTarget string
	root      string
	addr      string
	localBin  string
	remoteBin string
	platform  string
	tokenFile string
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
	fmt.Fprintln(out, "Run these commands from this machine. They copy a prebuilt daemon and start it without remote Go, npm, apt, brew, or internet.")
	fmt.Fprintf(out, "scp %s %s:%s\n", shellQuote(deploy.localBin), shellQuote(remote), shellQuote(deploy.remoteBin))
	fmt.Fprintf(out, "ssh %s %s\n", shellQuote(remote), shellQuote("chmod 0755 "+deploy.remoteBin))
	if startCmd != "" {
		fmt.Fprintf(out, "ssh %s %s\n", shellQuote(remote), shellQuote(startCmd))
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Then configure the host URL if needed:\n  remork host add %s --url http://HOST:%s\n", deploy.hostName, daemonPort(deploy.addr))
	fmt.Fprintf(out, "Verify:\n  remork daemon status %s\n", deploy.hostName)
}

func remoteStartCommand(deploy daemonDeployOptions) string {
	if deploy.root == "" {
		return ""
	}
	args := []string{deploy.remoteBin, "--root", deploy.root, "--addr", deploy.addr}
	if deploy.tokenFile != "" {
		args = append(args, "--token-file", deploy.tokenFile)
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return "nohup " + strings.Join(quoted, " ") + " </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid"
}

func filepathForDaemonBinary(platform string) string {
	if platform == "" {
		platform = runtime.GOOS + "-" + runtime.GOARCH
	}
	return "dist/remorkd-" + platform
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
