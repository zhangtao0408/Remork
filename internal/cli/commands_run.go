package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/exitcode"
	"remork/internal/preflight"
	"remork/internal/state"
	"remork/internal/syncer"
	"remork/internal/workspace"
)

func addRunCommand(root *cobra.Command, opts Options) {
	runCmd := newRunCommand(opts)
	root.AddCommand(runCmd)

	execCmd := newRunCommand(opts)
	execCmd.Use = "exec [flags] command"
	execCmd.Hidden = true
	root.AddCommand(execCmd)
}

func newRunCommand(opts Options) *cobra.Command {
	var remoteOnly bool
	var noSyncCheck bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "run [flags] command",
		Short: "Run a command in the remote workspace",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
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
				if decision.Warning != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), decision.Warning)
				}
			}

			command := runCommandArgs(args)
			result, err := runCtx.client.Exec(runCtx.binding.RemoteRoot, runCtx.binding.RemoteRoot, command, timeout.Milliseconds())
			if result.Stdout != "" {
				fmt.Fprint(cmd.OutOrStdout(), result.Stdout)
			}
			if result.Stderr != "" {
				fmt.Fprint(cmd.ErrOrStderr(), result.Stderr)
			}
			if err != nil {
				return err
			}
			if result.TimedOut {
				return codedCommandError{code: exitcode.Timeout, err: fmt.Errorf("remote command timed out")}
			}
			if result.ExitCode != 0 {
				return codedCommandError{code: exitcode.RemoteCommandFailed, err: fmt.Errorf("remote command failed with exit code %d", result.ExitCode)}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&remoteOnly, "remote-only", false, "Run without blocking on local pending changes")
	cmd.Flags().BoolVar(&noSyncCheck, "no-sync-check", false, "Skip local and remote workspace state checks")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Remote command timeout")
	return cmd
}

func runCommandArgs(args []string) []string {
	if len(args) == 1 {
		return []string{"sh", "-c", args[0]}
	}
	return args
}

type runContext struct {
	binding workspace.Binding
	client  client.Client
	runner  syncer.Runner
}

func newRunContext(opts Options) (runContext, error) {
	binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return runContext{}, fmt.Errorf("current directory is not bound to a remork workspace: %w", err)
	}
	store, err := configStore(opts)
	if err != nil {
		return runContext{}, err
	}
	cfg, err := store.Load()
	if err != nil {
		return runContext{}, err
	}
	host, ok := cfg.Hosts[binding.Host]
	if !ok {
		return runContext{}, fmt.Errorf("host %q is not configured", binding.Host)
	}
	token, err := auth.TokenFromEnv(host.TokenEnv)
	if err != nil {
		return runContext{}, err
	}
	c := client.NewWithOptions(client.Options{BaseURL: host.URL, ClientID: cfg.ClientID, Token: token})
	workspaceRef := binding.Host + ":" + binding.RemoteRoot
	runner := syncer.NewRunner(syncer.RunnerOptions{
		Client:       c,
		StateStore:   state.NewStore(binding.StateDir),
		LocalRoot:    localRoot,
		WorkspaceRef: workspaceRef,
		RemoteRoot:   binding.RemoteRoot,
	})
	return runContext{binding: binding, client: c, runner: runner}, nil
}
