package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/tui"
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
					err:  fmt.Errorf("remork setup requires an interactive terminal"),
					fix:  "run remork setup in a terminal, or use advanced commands such as remork host add and remork init",
				}
			}
			return runSetupScopeMenu(cmd, opts)
		},
	}
	root.AddCommand(cmd)
}

func runSetupScopeMenu(cmd *cobra.Command, opts Options) error {
	_ = opts
	plainRenderer(cmd, false).Section("Setup")
	plainRenderer(cmd, false).List("Choose what to set up", []string{
		"Connect this project",
		"Only prepare a server",
		"Repair an existing setup",
	})
	return nil
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
	url := strings.TrimSpace(values["url"])
	spec := DaemonDeploySpec{
		Action:    "install",
		HostName:  host,
		SSHTarget: strings.TrimSpace(values["ssh"]),
		Roots:     splitDaemonDeployRoots(values["roots"]),
		Addr:      strings.TrimSpace(values["addr"]),
		LocalBin:  strings.TrimSpace(values["local_bin"]),
		RemoteBin: strings.TrimSpace(values["remote_bin"]),
		URL:       url,
		TokenEnv:  strings.TrimSpace(values["token_env"]),
		NoProxy:   noProxy,
		Verify:    verify,
		Execute:   true,
	}
	return spec, HostConfigSpec{Name: host, URL: url, TokenEnv: spec.TokenEnv, NoProxy: noProxy}, nil
}

func setupPrepareServerFields(initial map[string]string) []tui.Field {
	if initial == nil {
		initial = map[string]string{}
	}
	return []tui.Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab", Initial: initial["host"]},
		{Key: "ssh", Label: "SSH target", Placeholder: "user@server", Initial: initial["ssh"]},
		{Key: "roots", Label: "Allowed roots", Placeholder: "/absolute/allowed/root", Initial: initial["roots"]},
		{Key: "url", Label: "Daemon URL", Placeholder: "http://server:17731", Initial: initial["url"]},
		{Key: "addr", Label: "Listen addr", Placeholder: "0.0.0.0:17731", Initial: firstNonEmpty(initial["addr"], "0.0.0.0:17731")},
		{Key: "local_bin", Label: "Local binary", Placeholder: "auto", Initial: initial["local_bin"]},
		{Key: "remote_bin", Label: "Remote binary", Placeholder: ".local/bin/remorkd", Initial: firstNonEmpty(initial["remote_bin"], ".local/bin/remorkd")},
		{Key: "token_env", Label: "Token env", Placeholder: "REMORK_TOKEN", Initial: initial["token_env"]},
		{Key: "no_proxy", Label: "Bypass proxy y/N", Placeholder: "no", Initial: firstNonEmpty(initial["no_proxy"], "no")},
		{Key: "verify", Label: "Verify y/N", Placeholder: "yes", Initial: firstNonEmpty(initial["verify"], "yes")},
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
