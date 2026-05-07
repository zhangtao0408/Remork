package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"remork/internal/client"
	"remork/internal/config"
	"remork/internal/exitcode"
	"remork/internal/limits"
	"remork/internal/output"
	"remork/internal/preflight"
	"remork/internal/progress"
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
					return silentCommandError{err: codedCommandError{code: decision.ExitCode, err: fmt.Errorf("%s", decision.Message)}}
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
			var runProgress *progress.TextReporter
			if commandInteractionMode(cmd, interactionRequest{}).RichOutput {
				runProgress = newRunProgress(cmd.ErrOrStderr(), commandColorMode(cmd))
			}
			result, err := runCtx.client.ExecContext(ctx, runCtx.binding.RemoteRoot, runCtx.binding.RemoteRoot, command, timeout.Milliseconds())
			if err != nil {
				retryErr := retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
					var retryErr error
					result, retryErr = active.client.ExecContext(ctx, active.binding.RemoteRoot, active.binding.RemoteRoot, command, timeout.Milliseconds())
					return retryErr
				})
				if retryErr != nil {
					err = retryErr
				} else {
					err = nil
				}
			}
			if runProgress != nil {
				if err != nil {
					runProgress.FailMessage("remote command failed")
				} else {
					runProgress.DoneMessage("remote command output ready")
				}
			}
			if result.Stdout != "" {
				fmt.Fprint(cmd.OutOrStdout(), result.Stdout)
			}
			result.Stderr = cleanRunStderr(result.Stderr)
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
		return runBashRCCommand(args[0])
	}
	return runBashRCCommand(shellJoin(args))
}

func runBashRCCommand(command string) []string {
	return []string{"bash", "-ic", command}
}

func shellJoin(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func newRunProgress(w io.Writer, color output.ColorMode) *progress.TextReporter {
	reporter := progress.NewTextReporter(w, progress.Options{Color: color})
	reporter.Start("remote command running; output is replayed after completion...", 1)
	return reporter
}

func cleanRunStderr(stderr string) string {
	if stderr == "" {
		return ""
	}
	lines := strings.SplitAfter(stderr, "\n")
	var b strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "bash: cannot set terminal process group ") && strings.Contains(trimmed, "Inappropriate ioctl for device") {
			continue
		}
		if trimmed == "bash: no job control in this shell" {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

type runContext struct {
	binding      workspace.Binding
	client       client.Client
	runner       syncer.Runner
	cfg          config.Config
	host         config.Host
	localRoot    string
	workspaceRef string
	baseURL      string
	clientID     string
	token        string
	noProxy      bool
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
		return runContext{}, codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("host %q is not configured", binding.Host),
			fix:  fmt.Sprintf("run remork host add %s --url URL", binding.Host),
		}
	}
	token, err := tokenFromHost(host)
	if err != nil {
		return runContext{}, tokenSourceCommandError(host, err)
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
	return runContext{binding: binding, client: c, runner: runner, cfg: cfg, host: host, localRoot: localRoot, workspaceRef: workspaceRef, baseURL: host.URL, clientID: cfg.ClientID, token: token, noProxy: host.NoProxy}, nil
}

func (ctx runContext) withUpdatedHost(host config.Host) runContext {
	ctx.host = host
	if ctx.cfg.Hosts == nil {
		ctx.cfg.Hosts = map[string]config.Host{}
	}
	ctx.cfg.Hosts[host.Name] = host
	token, _ := tokenFromHost(host)
	ctx.token = token
	ctx.client = clientForHost(host, ctx.cfg, token)
	ctx.baseURL = host.URL
	ctx.noProxy = host.NoProxy
	ctx.runner = syncer.NewRunner(syncer.RunnerOptions{
		Client:       ctx.client,
		StateStore:   state.NewStore(ctx.binding.StateDir),
		LocalRoot:    ctx.localRoot,
		WorkspaceRef: ctx.workspaceRef,
		RemoteRoot:   ctx.binding.RemoteRoot,
	})
	return ctx
}

func (ctx runContext) runnerWithProgress(reporter syncer.ProgressReporter) syncer.Runner {
	return syncer.NewRunner(syncer.RunnerOptions{
		Client:       ctx.client,
		StateStore:   state.NewStore(ctx.binding.StateDir),
		LocalRoot:    ctx.localRoot,
		WorkspaceRef: ctx.workspaceRef,
		RemoteRoot:   ctx.binding.RemoteRoot,
		Progress:     reporter,
	})
}
