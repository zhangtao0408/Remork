package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/remoteroot"
	"remork/internal/tui"
	"remork/internal/workspace"
)

func addInitCommand(root *cobra.Command, opts Options) {
	root.AddCommand(&cobra.Command{
		Use:   "init [host:/absolute/path]",
		Short: "Bind the current directory to a remote workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
				if !mode.Wizard {
					return codedCommandError{
						code: exitcode.InvalidUsageOrConfig,
						err:  fmt.Errorf("remork init requires host:/absolute/path in non-interactive mode; run remork init from an interactive terminal or pass remork init host:/absolute/path"),
						fix:  "pass remork init HOST:/absolute/path, or run remork init from an interactive terminal",
					}
				}
				initial := map[string]string{}
				for {
					hostName, remoteRoot, values, err := runInitPrompt(cmd, initial)
					if err != nil {
						return err
					}
					if err := initWorkspace(cmd, opts, hostName, remoteRoot); err != nil {
						plainErrRenderer(cmd, false).Error(err.Error(), commandErrorFix(err))
						plainErrRenderer(cmd, false).Step("update the form values and submit again")
						initial = values
						continue
					}
					return nil
				}
			}
			hostName, remoteRoot, err := config.ParseWorkspaceRef(args[0])
			if err != nil {
				return err
			}
			return initWorkspace(cmd, opts, hostName, remoteRoot)
		},
	})
}

func runInitPrompt(cmd *cobra.Command, initial map[string]string) (string, string, map[string]string, error) {
	r := plainErrRenderer(cmd, false)
	r.Section("Init workspace")
	r.Step("choose a configured host and remote workspace root")
	values, err := runTUIForm(cmd, "Init workspace", []tui.Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab", Initial: initial["host"]},
		{Key: "root", Label: "Workspace root", Placeholder: "/absolute/remote/workspace", Initial: initial["root"]},
	})
	if err != nil {
		return "", "", nil, err
	}
	hostName := strings.TrimSpace(values["host"])
	remoteRoot := strings.TrimSpace(values["root"])
	if hostName == "" || remoteRoot == "" {
		return "", "", values, fmt.Errorf("host and workspace root are required")
	}
	return hostName, remoteRoot, values, nil
}

func initWorkspace(cmd *cobra.Command, opts Options, hostName, remoteRoot string) error {
	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return codedCommandError{
				code: exitcode.InvalidUsageOrConfig,
				err:  fmt.Errorf("remork is not configured on this machine; run remork host add %s --url URL, or run remork daemon install %s --url URL --root /allowed/root", hostName, hostName),
				fix:  fmt.Sprintf("run remork host add %s --url URL", hostName),
			}
		}
		return err
	}
	host, ok := cfg.Hosts[hostName]
	if !ok {
		return fmt.Errorf("host %q is not configured", hostName)
	}
	status, err := opts.DaemonProbe.Status(cmd.Context(), host, cfg.ClientID)
	if err != nil {
		return err
	}
	ok, err = remoteRootAdvertised(status.Roots, remoteRoot)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("remote workspace %q is outside advertised allowed roots for host %q", remoteRoot, hostName)
	}
	if _, err := opts.DaemonProbe.Manifest(cmd.Context(), host, cfg, remoteRoot); err != nil {
		return fmt.Errorf("remote workspace %q is advertised but cannot be served by host %q: %w", remoteRoot, hostName, err)
	}
	localRoot, err := filepath.Abs(opts.WorkingDir)
	if err != nil {
		return err
	}
	workspaceID := stableWorkspaceID(hostName, host.URL, remoteRoot, localRoot)
	stateDir := filepath.Join(opts.HomeDir, ".remork", "state", workspaceID)
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	if err := workspace.WriteBinding(localRoot, workspace.Binding{
		Version:     1,
		Host:        hostName,
		RemoteRoot:  remoteRoot,
		WorkspaceID: workspaceID,
		StateDir:    stateDir,
	}); err != nil {
		return err
	}
	cfg.Workspaces[workspaceID] = config.Workspace{Host: hostName, RemoteRoot: remoteRoot, LocalRoot: localRoot}
	if err := store.Save(cfg); err != nil {
		return err
	}
	r := plainRenderer(cmd, false)
	r.Section("Workspace bound")
	r.KeyValue("host", hostName)
	r.KeyValue("workspace root", remoteRoot)
	r.KeyValue("local root", localRoot)
	return nil
}

func remoteRootAdvertised(roots []string, remoteRoot string) (bool, error) {
	allowed, err := remoteroot.NormalizeMany(roots)
	if err != nil {
		return false, err
	}
	return remoteroot.Contains(allowed, remoteRoot)
}

func stableWorkspaceID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%x:", len(part))
		h.Write([]byte(part))
		h.Write([]byte{':'})
	}
	return "ws_" + hex.EncodeToString(h.Sum(nil))[:16]
}
