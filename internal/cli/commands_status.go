package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"remork/internal/auth"
	"remork/internal/output"
	"remork/internal/state"
	"remork/internal/syncer"
	"remork/internal/workspace"
)

func addStatusCommand(root *cobra.Command, opts Options) {
	var jsonOut bool

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
			fmt.Fprintf(cmd.OutOrStdout(), "Next: %s\n", nextStatusAction(status))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
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
		return "resolve conflicts before sync/apply"
	case status.LocalChanges > 0:
		return "remork apply"
	case status.RemoteUpdates > 0:
		return "remork sync"
	default:
		return "up to date"
	}
}
