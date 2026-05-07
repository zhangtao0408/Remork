package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/preflight"
	"remork/internal/shellclient"
	"remork/internal/syncer"
)

func addShellCommand(root *cobra.Command, opts Options) {
	var remoteOnly bool
	var noSyncCheck bool
	var list bool
	var attachID string
	var killID string

	cmd := &cobra.Command{
		Use:   "shell [flags]",
		Short: "Open an interactive remote shell",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			modeCount := 0
			if list {
				modeCount++
			}
			if attachID != "" {
				modeCount++
			}
			if killID != "" {
				modeCount++
			}
			if modeCount > 1 {
				return fmt.Errorf("--list, --attach, and --kill are mutually exclusive")
			}
			runCtx, err := newRunContext(opts)
			if err != nil {
				return err
			}
			if list {
				err := listShellSessions(ctx, cmd, runCtx)
				if err != nil {
					return retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
						return listShellSessions(ctx, cmd, active)
					})
				}
				return nil
			}
			if killID != "" {
				if err := runCtx.client.KillShellSession(ctx, runCtx.binding.RemoteRoot, killID); err != nil {
					return retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
						return active.client.KillShellSession(ctx, active.binding.RemoteRoot, killID)
					})
				}
				r := plainRenderer(cmd, false)
				r.Section("Shell session")
				r.Success("killed " + killID)
				return nil
			}
			if err := requireInteractiveTerminal(cmd, "interactive shell"); err != nil {
				return codedCommandError{code: exitcode.InvalidUsageOrConfig, err: err}
			}
			if !remoteOnly && !noSyncCheck {
				status, err := runCtx.runner.Status(ctx)
				if err != nil {
					retryErr := retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
						var retryStatus syncer.Status
						var retryErr error
						retryStatus, retryErr = active.runner.Status(ctx)
						if retryErr == nil {
							runCtx = active
							status = retryStatus
						}
						return retryErr
					})
					if retryErr != nil {
						return retryErr
					}
				}
				decision := preflight.Decide(preflight.WorkspaceState{
					LocalDirty:  status.LocalChanges,
					RemoteStale: status.RemoteUpdates > 0,
					Conflicts:   status.Conflicts,
				}, preflight.Options{})
				if !decision.Allow {
					plainErrRenderer(cmd, false).Warning(decision.Message)
					return codedCommandError{code: decision.ExitCode, err: fmt.Errorf("%s", decision.Message)}
				}
				if status.RemoteUpdates > 0 {
					syncResult, err := runCtx.runner.Sync(ctx, syncer.SyncOptions{})
					if err != nil {
						retryErr := retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
							var retryResult syncer.Result
							var retryErr error
							retryResult, retryErr = active.runner.Sync(ctx, syncer.SyncOptions{})
							if retryErr == nil {
								runCtx = active
								syncResult = retryResult
							}
							return retryErr
						})
						if retryErr != nil {
							return retryErr
						}
					}
					if syncResult.Conflicts > 0 {
						msg := "Remote updates conflict with local files; resolve conflicts before running remote commands."
						plainErrRenderer(cmd, false).Error(msg, "run remork status")
						return codedCommandError{code: exitcode.Conflict, err: fmt.Errorf("%s", msg)}
					}
				}
			} else {
				decision := preflight.Decide(preflight.WorkspaceState{}, preflight.Options{
					RemoteOnly:  remoteOnly,
					NoSyncCheck: noSyncCheck,
				})
				if decision.Warning != "" {
					plainErrRenderer(cmd, false).Warning(decision.Warning)
				}
			}

			before, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
			if err != nil {
				retryErr := retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
					retryBefore, retryErr := active.client.ManifestContext(ctx, active.binding.RemoteRoot, ".")
					if retryErr == nil {
						runCtx = active
						before = retryBefore
					}
					return retryErr
				})
				if retryErr != nil {
					return retryErr
				}
			}
			err = shellclient.Run(ctx, shellclient.Options{
				BaseURL:   runCtx.baseURL,
				Root:      runCtx.binding.RemoteRoot,
				SessionID: attachID,
				ClientID:  runCtx.clientID,
				Token:     runCtx.token,
				NoProxy:   runCtx.noProxy,
				Stdin:     cmd.InOrStdin(),
				Stdout:    cmd.OutOrStdout(),
			})
			if err != nil {
				var disconnectErr shellclient.DisconnectError
				if errors.As(err, &disconnectErr) {
					plainErrRenderer(cmd, false).Warning("Shell connection closed; the remote session may still be running. Use `remork shell --list` and `remork shell --attach <id>` to reconnect, or `remork shell --kill <id>` to stop it.")
					return nil
				}
				return err
			}
			after, err := runCtx.client.ManifestContext(ctx, runCtx.binding.RemoteRoot, ".")
			if err != nil {
				retryErr := retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
					var retryErr error
					after, retryErr = active.client.ManifestContext(ctx, active.binding.RemoteRoot, ".")
					return retryErr
				})
				if retryErr != nil {
					return retryErr
				}
			}
			if before.Revision != after.Revision {
				plainErrRenderer(cmd, false).Warning("Remote workspace changed during shell session. Run remork sync to update local files.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&remoteOnly, "remote-only", false, "Open shell without blocking on local pending changes")
	cmd.Flags().BoolVar(&noSyncCheck, "no-sync-check", false, "Skip local and remote workspace state checks")
	cmd.Flags().BoolVar(&list, "list", false, "List durable remote shell sessions")
	cmd.Flags().StringVar(&attachID, "attach", "", "Attach to an existing remote shell session")
	cmd.Flags().StringVar(&killID, "kill", "", "Kill an existing remote shell session")
	root.AddCommand(cmd)
}

func listShellSessions(ctx context.Context, cmd *cobra.Command, runCtx runContext) error {
	sessions, err := runCtx.client.ShellSessions(ctx, runCtx.binding.RemoteRoot)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		plainRenderer(cmd, false).Empty("no shell sessions", "run remork shell")
		return nil
	}
	rows := make([][]string, 0, len(sessions))
	for _, session := range sessions {
		rows = append(rows, []string{session.ID, strings.Join(session.Command, " "), session.LastActive})
	}
	r := plainRenderer(cmd, false)
	r.Section("Shell sessions")
	r.Table([]string{"ID", "COMMAND", "LAST ACTIVE"}, rows)
	r.Command("remork shell --attach <session-id>")
	return nil
}
