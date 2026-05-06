package cli

import "github.com/spf13/cobra"

func addConflictCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "conflict PATH",
		Short: "Show conflict recovery steps for a path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			r := plainRenderer(cmd, false)
			r.Section("Conflict")
			r.KeyValue("Conflict", path)
			r.List("Recovery steps", []string{
				"Review local changes: " + pathCommand("diff", path),
				"Discard local edits back to synced base: " + pathCommand("restore", path),
				"Then: remork status",
				"If remote updates remain: remork sync",
				"Then continue or apply as appropriate: remork apply",
			})
			return nil
		},
	}
	root.AddCommand(cmd)
}
