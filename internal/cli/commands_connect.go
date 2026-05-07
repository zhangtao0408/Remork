package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/tui"
)

func addConnectCommand(root *cobra.Command, opts Options) {
	var spec ConnectSpec
	firstSync := true
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect this directory to an existing remorkd",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec.FirstSync = firstSync
			if spec.URL == "" {
				mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
				if mode.Wizard {
					return runConnectTUI(cmd, opts, spec)
				}
				return codedCommandError{code: exitcode.InvalidUsageOrConfig, err: fmt.Errorf("--url is required"), fix: "pass remork connect --url http://HOST:PORT"}
			}
			if err := ExecuteConnectSpec(opts, spec); err != nil {
				return err
			}
			r := plainRenderer(cmd, false)
			r.Section("Connected")
			r.KeyValue("host", firstNonEmpty(spec.HostName, "derived from URL"))
			r.Success("connected")
			if firstSync {
				cmd.Root().SetArgs([]string{"sync"})
				return cmd.Root().ExecuteContext(cmd.Context())
			}
			r.Next([]string{"remork sync"})
			return nil
		},
	}
	cmd.Flags().StringVar(&spec.URL, "url", "", "Daemon URL")
	cmd.Flags().StringVar(&spec.HostName, "host", "", "Saved host name")
	cmd.Flags().StringVar(&spec.Token, "token", "", "Daemon token to save locally")
	cmd.Flags().StringVar(&spec.TokenEnv, "token-env", "", "Environment variable containing the daemon token")
	cmd.Flags().StringVar(&spec.TokenFile, "token-file", "", "Local token file to read or write")
	cmd.Flags().BoolVar(&spec.NoProxy, "no-proxy", false, "Bypass proxies for this daemon")
	cmd.Flags().StringVar(&spec.SelectedRoot, "root", "", "Advertised allowed root to use as the base for relative workspace paths")
	cmd.Flags().StringVar(&spec.WorkspacePath, "workspace-path", "", "Workspace path, either relative to --root or absolute inside an advertised root")
	cmd.Flags().BoolVar(&firstSync, "first-sync", true, "Run remork sync after connecting")
	root.AddCommand(cmd)
}

func runConnectTUI(cmd *cobra.Command, opts Options, initial ConnectSpec) error {
	values, err := runTUIForm(cmd, "Connect to existing daemon", []tui.Field{
		{Section: "Daemon", Key: "url", Label: "Daemon URL", Placeholder: "http://server:17731", Initial: initial.URL, Help: "HTTP URL for an already running remorkd."},
		{Section: "Daemon", Key: "host", Label: "Host name", Placeholder: "auto", Initial: initial.HostName, Help: "Saved local name. Leave empty to derive one from the URL."},
		{Section: "Auth", Key: "token", Label: "Token", Initial: initial.Token, Help: "Optional. Leave empty for unauthenticated private-network daemons."},
		{Section: "Auth", Key: "token_file", Label: "Token file", Initial: initial.TokenFile, Help: "Optional. Defaults to ~/.remork/tokens/<host>.token when a token is entered."},
		{Section: "Workspace", Key: "root", Label: "Allowed root", Initial: initial.SelectedRoot, Help: "Advertised daemon root used as the base for relative workspace paths."},
		{Section: "Workspace", Key: "workspace_path", Label: "Workspace path", Initial: initial.WorkspacePath, Help: "Empty uses the allowed root; relative paths join under it; absolute paths must be inside an advertised root."},
		{Section: "Network", Key: "no_proxy", Label: "Bypass proxy y/N", Placeholder: "no", Initial: yesNo(initial.NoProxy), Help: "Use yes for VPN or private IPs that should bypass local proxy variables."},
		{Section: "First run", Key: "first_sync", Label: "Run first sync y/N", Placeholder: "yes", Initial: yesNo(initial.FirstSync), Help: "Download current remote files after binding."},
	})
	if err != nil {
		return err
	}
	noProxy, err := parseDaemonDeployBool(values["no_proxy"], "no proxy")
	if err != nil {
		return err
	}
	firstSync, err := parseDaemonDeployBool(values["first_sync"], "first sync")
	if err != nil {
		return err
	}
	spec := ConnectSpec{
		URL:           strings.TrimSpace(values["url"]),
		HostName:      strings.TrimSpace(values["host"]),
		Token:         strings.TrimSpace(values["token"]),
		TokenFile:     strings.TrimSpace(values["token_file"]),
		NoProxy:       noProxy,
		SelectedRoot:  strings.TrimSpace(values["root"]),
		WorkspacePath: strings.TrimSpace(values["workspace_path"]),
		FirstSync:     firstSync,
	}
	if err := ExecuteConnectSpec(opts, spec); err != nil {
		return err
	}
	plainRenderer(cmd, false).Success("connected")
	if firstSync {
		cmd.Root().SetArgs([]string{"sync"})
		return cmd.Root().ExecuteContext(cmd.Context())
	}
	plainRenderer(cmd, false).Next([]string{"remork sync"})
	return nil
}
