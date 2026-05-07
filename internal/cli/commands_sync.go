package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/progress"
	"remork/internal/state"
	"remork/internal/syncer"
	"remork/internal/workspace"
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
			binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
			if err != nil {
				err = unboundWorkspaceError(err)
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			store, err := configStore(opts)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			cfg, err := store.Load()
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			host, ok := cfg.Hosts[binding.Host]
			if !ok {
				err := codedCommandError{
					code: exitcode.InvalidUsageOrConfig,
					err:  fmt.Errorf("host %q is not configured", binding.Host),
					fix:  fmt.Sprintf("run remork host add %s --url URL", binding.Host),
				}
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			token, err := tokenFromHost(host)
			if err != nil {
				err = tokenSourceCommandError(host, err)
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
			workspaceRef := binding.Host + ":" + binding.RemoteRoot
			runner := syncer.NewRunner(syncer.RunnerOptions{
				Client:       clientForHost(host, cfg, token),
				StateStore:   state.NewStore(binding.StateDir),
				LocalRoot:    localRoot,
				WorkspaceRef: workspaceRef,
				RemoteRoot:   binding.RemoteRoot,
				Progress:     progress.NewTextReporter(cmd.OutOrStdout(), progress.Options{Quiet: quiet || jsonOut, Color: commandColorMode(cmd)}),
			})
			result, err := runner.Sync(cmd.Context(), syncer.SyncOptions{
				TargetPath: targetPath,
				Force:      force,
				Quiet:      quiet,
			})
			if err != nil {
				err = daemonReachabilityCommandError(err, "start remorkd, check VPN/firewall reachability, then rerun remork sync")
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
