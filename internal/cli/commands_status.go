package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/state"
	"remork/internal/syncer"
	"remork/internal/workspace"
)

func addStatusCommand(root *cobra.Command, opts Options) {
	var jsonOut bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local, remote, conflict, and large-file state",
		Args:  noArgsJSON(&jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, err := newRunContext(opts)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			status, err := runCtx.runner.Status(cmd.Context())
			if err != nil {
				retryErr := retryAfterTokenFileUpdate(cmd, opts, runCtx, err, func(active runContext) error {
					var retryStatus syncer.Status
					var retryErr error
					retryStatus, retryErr = active.runner.Status(cmd.Context())
					if retryErr == nil {
						status = retryStatus
					}
					return retryErr
				})
				if retryErr != nil {
					if isAuthHTTPError(err) {
						err = retryErr
					} else {
						err = daemonReachabilityCommandError(retryErr, "start remorkd, check VPN/firewall reachability, then run remork doctor")
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
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), status)
			}
			r := plainRenderer(cmd, false)
			r.Section("Workspace status")
			r.KeyValue("Workspace", status.Workspace)
			r.KeyValue("Local", status.LocalRoot)
			r.KeyValue("Clean", status.Clean)
			r.KeyValue("Local changes", status.LocalChanges)
			r.KeyValue("Remote updates", status.RemoteUpdates)
			r.KeyValue("Conflicts", status.Conflicts)
			r.KeyValue("Large placeholders", status.LargePlaceholders)
			writeStatusPaths(cmd, status, verbose)
			r.KeyValue("Next", nextStatusAction(status))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print detailed text status")
	root.AddCommand(cmd)
}

func newBoundSyncRunner(opts Options) (syncer.Runner, error) {
	binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return syncer.Runner{}, unboundWorkspaceError(err)
	}
	store, err := configStore(opts)
	if err != nil {
		return syncer.Runner{}, err
	}
	cfg, err := store.Load()
	if err != nil {
		return syncer.Runner{}, err
	}
	host, ok := cfg.Hosts[binding.Host]
	if !ok {
		return syncer.Runner{}, codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("host %q is not configured", binding.Host),
			fix:  fmt.Sprintf("run remork host add %s --url URL", binding.Host),
		}
	}
	token, err := tokenFromHost(host)
	if err != nil {
		return syncer.Runner{}, tokenSourceCommandError(host, err)
	}
	workspaceRef := binding.Host + ":" + binding.RemoteRoot
	return syncer.NewRunner(syncer.RunnerOptions{
		Client:       clientForHost(host, cfg, token),
		StateStore:   state.NewStore(binding.StateDir),
		LocalRoot:    localRoot,
		WorkspaceRef: workspaceRef,
		RemoteRoot:   binding.RemoteRoot,
	}), nil
}

func nextStatusAction(status syncer.Status) string {
	switch {
	case status.Conflicts > 0:
		if len(status.ConflictPaths) > 0 {
			return pathCommand("conflict", status.ConflictPaths[0])
		}
		return "remork conflict <path>"
	case status.LocalChanges > 0:
		return "remork apply"
	case status.RemoteUpdates > 0:
		return "remork sync"
	default:
		return "up to date"
	}
}

func writeStatusPaths(cmd *cobra.Command, status syncer.Status, verbose bool) {
	r := plainRenderer(cmd, false)
	if status.Conflicts > 0 {
		paths, more := limitedPaths(status.ConflictPaths, verbose)
		r.List("Conflict paths:", paths)
		if more > 0 {
			r.Warning(fmt.Sprintf("%d more conflict path(s); use remork status --verbose", more))
		}
		if len(status.ConflictPaths) > 0 {
			path := status.ConflictPaths[0]
			r.List("Conflict recovery", []string{
				"Review: " + pathCommand("conflict", path),
				"Discard local edits back to synced base after review: " + pathCommand("restore", path),
				"Then run: remork status",
				"If remote updates remain: remork sync",
			})
		}
	}
	if verbose && status.LocalChanges > 0 {
		r.List("Changed paths:", status.ChangedPaths)
	}
}

func limitedPaths(paths []string, verbose bool) ([]string, int) {
	if verbose || len(paths) <= 10 {
		return paths, 0
	}
	return paths[:10], len(paths) - 10
}

func pathCommand(cmd string, path string) string {
	return fmt.Sprintf("remork %s -- %s", cmd, shellQuotePath(path))
}

func shellQuotePath(path string) string {
	if path == "" || strings.IndexFunc(path, func(r rune) bool {
		return !isShellSafePathChar(r)
	}) >= 0 {
		return shellQuote(path)
	}
	return path
}

func isShellSafePathChar(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		strings.ContainsRune("_-./:@%+=,", r)
}
