package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/prompt"
	"remork/internal/syncer"
)

func addPullCommand(root *cobra.Command, opts Options) {
	var force bool
	var quiet bool
	var jsonOut bool
	var includeLarge bool

	cmd := &cobra.Command{
		Use:   "pull <path>",
		Short: "Fetch a specific file or directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newBoundSyncRunner(opts)
			if err != nil {
				return err
			}
			result, err := runner.Pull(context.Background(), args[0], syncer.PullOptions{
				Force:        force,
				Quiet:        quiet,
				IncludeLarge: includeLarge,
				In:           cmd.InOrStdin(),
				Out:          cmd.ErrOrStderr(),
			})
			if err != nil {
				if errors.Is(err, prompt.ErrPromptRequired) {
					return codedCommandError{code: exitcode.PromptRequired, err: err}
				}
				return err
			}
			if result.Conflicts > 0 {
				return codedCommandError{code: exitcode.Conflict, err: fmt.Errorf("pull completed with %d conflicts", result.Conflicts)}
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), result)
			}
			if !quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "pull complete: downloaded %d, meta %d, skipped %d, conflicts %d\n", result.Downloaded, result.MetaWritten, result.Skipped, result.Conflicts)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Confirm large-file downloads and overwrite dirty local files")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress text summary")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&includeLarge, "include-large", false, "Download large files instead of placeholders")
	_ = cmd.Flags().MarkHidden("include-large")
	root.AddCommand(cmd)
}
