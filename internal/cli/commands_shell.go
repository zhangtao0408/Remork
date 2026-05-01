package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/preflight"
	"remork/internal/shellclient"
	"remork/internal/syncer"
)

func addShellCommand(root *cobra.Command, opts Options) {
	var remoteOnly bool
	var noSyncCheck bool

	cmd := &cobra.Command{
		Use:   "shell [flags]",
		Short: "Open an interactive remote shell",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			runCtx, err := newRunContext(opts)
			if err != nil {
				return err
			}
			if !remoteOnly && !noSyncCheck {
				status, err := runCtx.runner.Status(ctx)
				if err != nil {
					return err
				}
				decision := preflight.Decide(preflight.WorkspaceState{
					LocalDirty:  status.LocalChanges,
					RemoteStale: status.RemoteUpdates > 0,
					Conflicts:   status.Conflicts,
				}, preflight.Options{})
				if !decision.Allow {
					fmt.Fprintln(cmd.ErrOrStderr(), decision.Message)
					return codedCommandError{code: decision.ExitCode, err: fmt.Errorf("%s", decision.Message)}
				}
				if status.RemoteUpdates > 0 {
					syncResult, err := runCtx.runner.Sync(ctx, syncer.SyncOptions{})
					if err != nil {
						return err
					}
					if syncResult.Conflicts > 0 {
						msg := "Remote updates conflict with local files; resolve conflicts before running remote commands."
						fmt.Fprintln(cmd.ErrOrStderr(), msg)
						return codedCommandError{code: exitcode.Conflict, err: fmt.Errorf("%s", msg)}
					}
				}
			} else {
				decision := preflight.Decide(preflight.WorkspaceState{}, preflight.Options{
					RemoteOnly:  remoteOnly,
					NoSyncCheck: noSyncCheck,
				})
				if remoteOnly {
					fmt.Fprintln(cmd.ErrOrStderr(), "Remote-only shell: local pending changes are ignored.")
				} else if decision.Warning != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), decision.Warning)
				}
			}

			before, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
			if err != nil {
				return err
			}
			err = shellclient.Run(ctx, shellclient.Options{
				BaseURL:  runCtx.baseURL,
				Root:     runCtx.binding.RemoteRoot,
				ClientID: runCtx.clientID,
				Token:    runCtx.token,
				NoProxy:  runCtx.noProxy,
				Stdin:    cmd.InOrStdin(),
				Stdout:   cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}
			after, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
			if err != nil {
				return err
			}
			if before.Revision != after.Revision {
				fmt.Fprintln(cmd.ErrOrStderr(), "Remote workspace changed during shell session. Run remork sync to update local files.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&remoteOnly, "remote-only", false, "Open shell without blocking on local pending changes")
	cmd.Flags().BoolVar(&noSyncCheck, "no-sync-check", false, "Skip local and remote workspace state checks")
	root.AddCommand(cmd)
}
