package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewRootCommand(version string) *cobra.Command {
	root := &cobra.Command{Use: "remork", SilenceUsage: true}
	root.AddCommand(&cobra.Command{
		Use: "version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "remork "+version)
		},
	})
	root.AddCommand(&cobra.Command{
		Use:  "status [workspace]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "workspace %s\n", args[0])
			return nil
		},
	})
	return root
}
