package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"remork/internal/config"
)

func addHostCommand(root *cobra.Command, opts Options) {
	host := &cobra.Command{
		Use:   "host",
		Short: "Manage daemon endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listHosts(cmd, opts)
		},
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
	list := &cobra.Command{
		Use:   "list",
		Short: "List configured daemon endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listHosts(cmd, opts)
		},
	}
	remove := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a daemon endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := configStore(opts)
			if err != nil {
				return err
			}
			cfg, err := store.LoadOrDefault()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.Hosts[name]; !ok {
				return fmt.Errorf("host %q is not configured", name)
			}
			delete(cfg.Hosts, name)
			if err := store.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed host %s\n", name)
			return nil
		},
	}
	host.AddCommand(add)
	host.AddCommand(list)
	host.AddCommand(remove)
	root.AddCommand(host)
}

func listHosts(cmd *cobra.Command, opts Options) error {
	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	if len(cfg.Hosts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no hosts configured")
		return nil
	}
	names := make([]string, 0, len(cfg.Hosts))
	for name := range cfg.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "name\turl\ttoken_env\tflags")
	for _, name := range names {
		host := cfg.Hosts[name]
		flags := ""
		if host.NoProxy {
			flags = "no_proxy"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, host.URL, host.TokenEnv, flags)
	}
	return tw.Flush()
}
