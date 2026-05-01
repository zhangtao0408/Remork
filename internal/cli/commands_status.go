package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/auth"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, err := newBoundSyncRunner(opts)
			if err != nil {
				return err
			}
			status, err := runner.Status(cmd.Context())
			if err != nil {
				return err
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), status)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", status.Workspace)
			fmt.Fprintf(cmd.OutOrStdout(), "Local: %s\n", status.LocalRoot)
			fmt.Fprintf(cmd.OutOrStdout(), "Clean: %d\n", status.Clean)
			fmt.Fprintf(cmd.OutOrStdout(), "Local changes: %d\n", status.LocalChanges)
			fmt.Fprintf(cmd.OutOrStdout(), "Remote updates: %d\n", status.RemoteUpdates)
			fmt.Fprintf(cmd.OutOrStdout(), "Conflicts: %d\n", status.Conflicts)
			fmt.Fprintf(cmd.OutOrStdout(), "Large placeholders: %d\n", status.LargePlaceholders)
			writeStatusPaths(cmd, status, verbose)
			fmt.Fprintf(cmd.OutOrStdout(), "Next: %s\n", nextStatusAction(status))
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
		return syncer.Runner{}, fmt.Errorf("current directory is not bound to a remork workspace: %w", err)
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
		return syncer.Runner{}, fmt.Errorf("host %q is not configured", binding.Host)
	}
	token, err := auth.TokenFromEnv(host.TokenEnv)
	if err != nil {
		return syncer.Runner{}, err
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
	if status.Conflicts > 0 {
		paths, more := limitedPaths(status.ConflictPaths, verbose)
		fmt.Fprintln(cmd.OutOrStdout(), "Conflict paths:")
		for _, path := range paths {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", path)
		}
		if more > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  ... %d more (use remork status --verbose)\n", more)
		}
		if len(status.ConflictPaths) > 0 {
			path := status.ConflictPaths[0]
			fmt.Fprintf(cmd.OutOrStdout(), "Review: %s\n", pathCommand("conflict", path))
			fmt.Fprintf(cmd.OutOrStdout(), "Discard local edits back to synced base after review: %s\n", pathCommand("restore", path))
			fmt.Fprintln(cmd.OutOrStdout(), "Then run: remork status")
			fmt.Fprintln(cmd.OutOrStdout(), "If remote updates remain: remork sync")
		}
	}
	if verbose && status.LocalChanges > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Changed paths:")
		for _, path := range status.ChangedPaths {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", path)
		}
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
