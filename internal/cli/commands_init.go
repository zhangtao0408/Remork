package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"remork/internal/config"
	"remork/internal/remoteroot"
	"remork/internal/workspace"
)

func addInitCommand(root *cobra.Command, opts Options) {
	root.AddCommand(&cobra.Command{
		Use:   "init host:/absolute/path",
		Short: "Bind the current directory to a remote workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hostName, remoteRoot, err := config.ParseWorkspaceRef(args[0])
			if err != nil {
				return err
			}
			store, err := configStore(opts)
			if err != nil {
				return err
			}
			cfg, err := store.Load()
			if err != nil {
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
			return workspace.WriteBinding(localRoot, workspace.Binding{
				Version:     1,
				Host:        hostName,
				RemoteRoot:  remoteRoot,
				WorkspaceID: workspaceID,
				StateDir:    stateDir,
			})
		},
	})
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
