package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/client"
	"remork/internal/exitcode"
	"remork/internal/limits"
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
			if cmd.Flags().Changed("timeout") && timeout <= 0 {
				return codedCommandError{
					code: exitcode.InvalidUsageOrConfig,
					err:  fmt.Errorf("--timeout must be greater than 0"),
					fix:  "pass a positive duration such as --timeout 30s, or omit --timeout for the default",
				}
			}
			ctx := cmd.Context()
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
					plainErrRenderer(cmd, false).Warning(decision.Message)
					return silentCommandError{err: codedCommandError{code: decision.ExitCode, err: fmt.Errorf("%s", decision.Message)}}
				}
				if status.RemoteUpdates > 0 {
					syncResult, err := runCtx.runner.Sync(ctx, syncer.SyncOptions{})
					if err != nil {
						return err
					}
					if syncResult.Conflicts > 0 {
						msg := "Remote updates conflict with local files; resolve conflicts before running remote commands."
						plainErrRenderer(cmd, false).Error(msg, "run remork status")
						return silentCommandError{err: codedCommandError{code: exitcode.Conflict, err: fmt.Errorf("%s", msg)}}
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

			command := runCommandArgs(args)
			if commandInteractionMode(cmd, interactionRequest{}).RichOutput {
				plainErrRenderer(cmd, false).Step("remote command running; output is replayed after completion...")
			}
			result, err := runCtx.client.ExecContext(ctx, runCtx.binding.RemoteRoot, runCtx.binding.RemoteRoot, command, timeout.Milliseconds())
			if result.Stdout != "" {
				fmt.Fprint(cmd.OutOrStdout(), result.Stdout)
			}
			if result.Stderr != "" {
				fmt.Fprint(cmd.ErrOrStderr(), result.Stderr)
			}
			if result.StdoutTruncated {
				plainErrRenderer(cmd, false).Warning(fmt.Sprintf("stdout truncated after %d bytes", limits.MaxExecOutputBytes))
			}
			if result.StderrTruncated {
				plainErrRenderer(cmd, false).Warning(fmt.Sprintf("stderr truncated after %d bytes", limits.MaxExecOutputBytes))
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
	binding  workspace.Binding
	client   client.Client
	runner   syncer.Runner
	baseURL  string
	clientID string
	token    string
	noProxy  bool
}

func newRunContext(opts Options) (runContext, error) {
	binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return runContext{}, unboundWorkspaceError(err)
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
	c := clientForHost(host, cfg, token)
	workspaceRef := binding.Host + ":" + binding.RemoteRoot
	runner := syncer.NewRunner(syncer.RunnerOptions{
		Client:       c,
		StateStore:   state.NewStore(binding.StateDir),
		LocalRoot:    localRoot,
		WorkspaceRef: workspaceRef,
		RemoteRoot:   binding.RemoteRoot,
	})
	return runContext{binding: binding, client: c, runner: runner, baseURL: host.URL, clientID: cfg.ClientID, token: token, noProxy: host.NoProxy}, nil
}
