package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"remork/internal/workspace"
)

func addWorkspaceCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Inspect or remove local bindings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
			if err != nil {
				return fmt.Errorf("current directory is not bound to a remork workspace: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "local: %s\n", localRoot)
			fmt.Fprintf(out, "host: %s\n", binding.Host)
			fmt.Fprintf(out, "remote_root: %s\n", binding.RemoteRoot)
			fmt.Fprintf(out, "workspace_id: %s\n", binding.WorkspaceID)
			fmt.Fprintf(out, "state_dir: %s\n", binding.StateDir)
			return nil
		},
	}
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove the local workspace binding marker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
			if err != nil {
				return fmt.Errorf("current directory is not bound to a remork workspace: %w", err)
			}
			marker := filepath.Join(localRoot, workspace.MarkerName)
			if err := os.Remove(marker); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed workspace binding %s\n", marker)
			return nil
		},
	}
	cmd.AddCommand(remove)
	root.AddCommand(cmd)
}
