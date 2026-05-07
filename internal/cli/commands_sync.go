package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/progress"
	"remork/internal/syncer"
)

func addSyncCommand(root *cobra.Command, opts Options) {
	var jsonOut bool
	var force bool
	var quiet bool

	cmd := &cobra.Command{
		Use:   "sync [path]",
		Short: "Sync remote files into the local working copy",
		Args:  maxArgsJSON(1, &jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, err := newRunContext(opts)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			targetPath := ""
			if len(args) == 1 {
				targetPath = args[0]
			}
			if err := validateWorkspacePathArg(targetPath); err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			runner := runCtx.runnerWithProgress(progress.NewTextReporter(cmd.OutOrStdout(), progress.Options{Quiet: quiet || jsonOut, Color: commandColorMode(cmd)}))
			result, err := runner.Sync(cmd.Context(), syncer.SyncOptions{
				TargetPath: targetPath,
				Force:      force,
				Quiet:      quiet,
			})
			if err != nil {
				retryErr := retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
					runner := active.runnerWithProgress(progress.NewTextReporter(cmd.OutOrStdout(), progress.Options{Quiet: quiet || jsonOut, Color: commandColorMode(cmd)}))
					var retryErr error
					result, retryErr = runner.Sync(cmd.Context(), syncer.SyncOptions{
						TargetPath: targetPath,
						Force:      force,
						Quiet:      quiet,
					})
					return retryErr
				})
				if retryErr != nil {
					if isAuthHTTPError(err) {
						err = retryErr
					} else {
						err = daemonReachabilityCommandError(retryErr, "start remorkd, check VPN/firewall reachability, then rerun remork sync")
					}
				} else {
					err = nil
				}
			}
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			if result.Conflicts > 0 {
				if jsonOut {
					if writeErr := output.WriteJSON(cmd.OutOrStdout(), result); writeErr != nil {
						return writeErr
					}
				} else if !quiet {
					writeConflictSummary(cmd, "sync", result.Conflicts)
				}
				err := codedCommandError{
					code: exitcode.Conflict,
					err:  fmt.Errorf("sync completed with %d conflicts", result.Conflicts),
					fix:  "run remork status, resolve conflicts, then rerun remork sync",
				}
				if jsonOut {
					return silentCommandError{err: err}
				}
				return err
			}
			if jsonOut {
				if writeErr := output.WriteJSON(cmd.OutOrStdout(), result); writeErr != nil {
					return writeErr
				}
			} else if !quiet {
				r := plainRenderer(cmd, false)
				r.Section("Sync complete")
				r.KeyValue("downloaded", result.Downloaded)
				r.KeyValue("meta", result.MetaWritten)
				r.KeyValue("deleted", result.Deleted)
				r.KeyValue("conflicts", result.Conflicts)
				r.Success(fmt.Sprintf("downloaded %d, meta %d, deleted %d, conflicts %d", result.Downloaded, result.MetaWritten, result.Deleted, result.Conflicts))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite dirty local files")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress text summary")
	root.AddCommand(cmd)
}
