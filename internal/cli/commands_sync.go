package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/output"
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
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
			if err != nil {
				return fmt.Errorf("current directory is not bound to a remork workspace: %w", err)
			}
			store, err := configStore(opts)
			if err != nil {
				return err
			}
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			host, ok := cfg.Hosts[binding.Host]
			if !ok {
				return fmt.Errorf("host %q is not configured", binding.Host)
			}
			token, err := auth.TokenFromEnv(host.TokenEnv)
			if err != nil {
				return err
			}
			targetPath := ""
			if len(args) == 1 {
				targetPath = args[0]
			}
			workspaceRef := binding.Host + ":" + binding.RemoteRoot
			runner := syncer.NewRunner(syncer.RunnerOptions{
				Client:       client.NewWithOptions(client.Options{BaseURL: host.URL, ClientID: cfg.ClientID, Token: token}),
				StateStore:   state.NewStore(binding.StateDir),
				LocalRoot:    localRoot,
				WorkspaceRef: workspaceRef,
				RemoteRoot:   binding.RemoteRoot,
			})
			result, err := runner.Sync(context.Background(), syncer.SyncOptions{
				TargetPath: targetPath,
				Force:      force,
				Quiet:      quiet,
			})
			if err != nil {
				return err
			}
			if result.Conflicts > 0 {
				return fmt.Errorf("sync completed with %d conflicts", result.Conflicts)
			}
			if jsonOut {
				if writeErr := output.WriteJSON(cmd.OutOrStdout(), result); writeErr != nil {
					return writeErr
				}
			} else if !quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "sync complete: downloaded %d, meta %d, deleted %d, conflicts %d\n", result.Downloaded, result.MetaWritten, result.Deleted, result.Conflicts)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite dirty local files")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress text summary")
	root.AddCommand(cmd)
}
