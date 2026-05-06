package cli

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/output"
)

func addHostCommand(root *cobra.Command, opts Options) {
	var listJSON bool
	host := &cobra.Command{
		Use:   "host",
		Short: "Manage daemon endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listHosts(cmd, opts, listJSON)
		},
	}
	var addJSON bool
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
				err := codedCommandError{
					code: exitcode.InvalidUsageOrConfig,
					err:  fmt.Errorf("--url is required"),
					fix:  "pass remork host add " + args[0] + " --url http://HOST:PORT",
				}
				if addJSON {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			if err := validateDaemonURL(url); err != nil {
				if addJSON {
					return writeJSONCommandError(cmd, codedCommandError{code: exitcode.InvalidUsageOrConfig, err: err, fix: "pass an http:// or https:// daemon URL"})
				}
				return err
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
			if err := store.Save(cfg); err != nil {
				return err
			}
			if addJSON {
				return output.WriteJSON(cmd.OutOrStdout(), cfg.Hosts[name])
			}
			r := plainRenderer(cmd, false)
			r.Section("Host saved")
			r.KeyValue("name", name)
			r.KeyValue("url", url)
			r.KeyValue("next", "remork daemon status "+name)
			r.Success("saved host " + name)
			return nil
		},
	}
	add.Flags().String("url", "", "Daemon URL")
	add.Flags().String("token-env", "", "Environment variable containing the daemon token")
	add.Flags().Bool("no-proxy", false, "Bypass proxies for this host")
	add.Flags().BoolVar(&addJSON, "json", false, "Print JSON output")
	list := &cobra.Command{
		Use:   "list",
		Short: "List configured daemon endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return listHosts(cmd, opts, listJSON)
		},
	}
	list.Flags().BoolVar(&listJSON, "json", false, "Print JSON output")
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

func validateDaemonURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid daemon URL %q: %w", value, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("daemon URL must include http:// or https://")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("daemon URL must include a host")
	}
	return nil
}

func listHosts(cmd *cobra.Command, opts Options, jsonOut bool) error {
	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	if jsonOut {
		return output.WriteJSON(cmd.OutOrStdout(), struct {
			Hosts map[string]config.Host `json:"hosts"`
		}{Hosts: cfg.Hosts})
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
