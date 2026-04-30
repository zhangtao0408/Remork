package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/config"
)

func addHostCommand(root *cobra.Command, opts Options) {
	host := &cobra.Command{
		Use:   "host",
		Short: "Manage daemon endpoints",
	}
	add := &cobra.Command{
		Use:   "add NAME --url URL",
		Short: "Add a daemon endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := cmd.Flags().GetString("url")
			if err != nil {
				return err
			}
			if url == "" {
				return fmt.Errorf("--url is required")
			}
			tokenEnv, err := cmd.Flags().GetString("token-env")
			if err != nil {
				return err
			}
			noProxy, err := cmd.Flags().GetBool("no-proxy")
			if err != nil {
				return err
			}
			store, err := configStore(opts)
			if err != nil {
				return err
			}
			cfg, err := store.LoadOrDefault()
			if err != nil {
				return err
			}
			name := args[0]
			cfg.Hosts[name] = config.Host{Name: name, URL: url, TokenEnv: tokenEnv, NoProxy: noProxy}
			return store.Save(cfg)
		},
	}
	add.Flags().String("url", "", "Daemon URL")
	add.Flags().String("token-env", "", "Environment variable containing the daemon token")
	add.Flags().Bool("no-proxy", false, "Bypass proxies for this host")
	host.AddCommand(add)
	root.AddCommand(host)
}
