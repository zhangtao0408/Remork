package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
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
	return codedCommandError{
		code: exitcode.InvalidUsageOrConfig,
		err:  fmt.Errorf("interactive connect is not implemented yet"),
		fix:  "pass --url and --workspace-path, or use remork host add and remork init",
	}
}
