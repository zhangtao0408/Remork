package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"remork/internal/config"
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
			status, err := opts.DaemonProbe.Status(context.Background(), host, cfg.ClientID)
			if err != nil {
				return err
			}
			if !containsRoot(status.Roots, remoteRoot) {
				return fmt.Errorf("remote root %q is not advertised by host %q", remoteRoot, hostName)
			}
			localRoot, err := filepath.Abs(opts.WorkingDir)
			if err != nil {
				return err
			}
			workspaceID := stableWorkspaceID(hostName, remoteRoot)
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

func containsRoot(roots []string, root string) bool {
	for _, candidate := range roots {
		if candidate == root {
			return true
		}
	}
	return false
}

func stableWorkspaceID(host, remoteRoot string) string {
	sum := sha256.Sum256([]byte(host + "\x00" + remoteRoot))
	return "ws_" + hex.EncodeToString(sum[:])[:16]
}
