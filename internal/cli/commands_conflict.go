package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func addConflictCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "conflict PATH",
		Short: "Show conflict recovery steps for a path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			fmt.Fprintf(cmd.OutOrStdout(), "Conflict: %s\n", path)
			fmt.Fprintf(cmd.OutOrStdout(), "Review local changes: %s\n", pathCommand("diff", path))
			fmt.Fprintf(cmd.OutOrStdout(), "Discard local edits back to synced base: %s\n", pathCommand("restore", path))
			fmt.Fprintln(cmd.OutOrStdout(), "Then: remork status")
			fmt.Fprintln(cmd.OutOrStdout(), "If remote updates remain: remork sync")
			fmt.Fprintln(cmd.OutOrStdout(), "Then continue or apply as appropriate: remork apply")
			return nil
		},
	}
	root.AddCommand(cmd)
}
