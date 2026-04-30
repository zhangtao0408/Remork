package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"remork/internal/apply"
	"remork/internal/client"
	"remork/internal/exitcode"
	"remork/internal/output"
	"remork/internal/state"
	"remork/internal/syncer"
	"remork/internal/workspace"
)

type codedCommandError struct {
	code int
	err  error
}

func (e codedCommandError) Error() string {
	return e.err.Error()
}

func (e codedCommandError) Unwrap() error {
	return e.err
}

func (e codedCommandError) ExitCode() int {
	return e.code
}

func addApplyCommand(root *cobra.Command, opts Options) {
	var dryRun bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Write local changes to the remote after base checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			runner, binding, localRoot, workspaceRef, err := boundApplyContext(opts)
			if err != nil {
				return err
			}
			snap, err := state.NewStore(binding.StateDir).Load(workspaceRef)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				snap = state.Snapshot{WorkspaceRef: workspaceRef, Entries: map[string]state.TrackedFile{}}
			}
			changeset, skipped, err := syncer.BuildChangeset(localRoot, snap)
			if err != nil {
				return err
			}
			summary := summarizeApplyPlan(changeset.Changes, skipped)
			if !jsonOut {
				printApplyPlan(cmd, summary)
			}
			if dryRun {
				if jsonOut {
					return output.WriteJSON(cmd.OutOrStdout(), applyJSONResult{
						ID:      changeset.ID,
						Plan:    summary,
						Skipped: skipped,
						DryRun:  true,
					})
				}
				return nil
			}
			if len(changeset.Changes) == 0 {
				if jsonOut {
					return output.WriteJSON(cmd.OutOrStdout(), applyJSONResult{
						ID:      changeset.ID,
						Plan:    summary,
						Skipped: skipped,
						Applied: 0,
					})
				}
				if !jsonOut {
					fmt.Fprintln(cmd.OutOrStdout(), "applied 0")
				}
				return nil
			}
			result, err := runner.ApplyChangeset(changeset)
			if err != nil {
				if isApplyConflict(err, result) {
					writeApplyConflict(cmd, result.Conflicts, jsonOut)
					return applyConflictError(result.Conflicts)
				}
				return err
			}
			if !result.Applied {
				writeApplyConflict(cmd, result.Conflicts, jsonOut)
				return applyConflictError(result.Conflicts)
			}
			if _, err := runner.Sync(context.Background(), syncer.SyncOptions{Force: true}); err != nil {
				return err
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), applyJSONResult{
					ID:      changeset.ID,
					Plan:    summary,
					Skipped: skipped,
					Applied: len(changeset.Changes),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "applied %d\n", len(changeset.Changes))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the apply plan without writing remote files")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	root.AddCommand(cmd)
}

type applyJSONResult struct {
	ID      string                 `json:"id"`
	Plan    map[string]int         `json:"plan"`
	Skipped []syncer.SkippedChange `json:"skipped"`
	DryRun  bool                   `json:"dry_run,omitempty"`
	Applied int                    `json:"applied,omitempty"`
}

func boundApplyContext(opts Options) (syncer.Runner, workspace.Binding, string, string, error) {
	binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return syncer.Runner{}, workspace.Binding{}, "", "", fmt.Errorf("current directory is not bound to a remork workspace: %w", err)
	}
	runner, err := newBoundSyncRunner(opts)
	if err != nil {
		return syncer.Runner{}, workspace.Binding{}, "", "", err
	}
	return runner, binding, localRoot, binding.Host + ":" + binding.RemoteRoot, nil
}

func summarizeApplyPlan(changes []apply.Change, skipped []syncer.SkippedChange) map[string]int {
	summary := map[string]int{
		"create":  0,
		"update":  0,
		"delete":  0,
		"skipped": len(skipped),
	}
	for _, change := range changes {
		summary[string(change.Kind)]++
	}
	return summary
}

func printApplyPlan(cmd *cobra.Command, summary map[string]int) {
	fmt.Fprintf(cmd.OutOrStdout(), "apply plan: create %d, update %d, delete %d, skipped %d\n", summary["create"], summary["update"], summary["delete"], summary["skipped"])
}

func isApplyConflict(err error, result apply.Result) bool {
	var httpErr *client.HTTPError
	return len(result.Conflicts) > 0 || errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict
}

func applyConflictError(paths []string) error {
	if len(paths) == 0 {
		return codedCommandError{code: exitcode.Conflict, err: errors.New("conflict")}
	}
	return codedCommandError{code: exitcode.Conflict, err: fmt.Errorf("conflict: %v", paths)}
}

func writeApplyConflict(cmd *cobra.Command, paths []string, jsonOut bool) {
	if jsonOut {
		_ = output.WriteJSON(cmd.OutOrStdout(), struct {
			Conflicts []string `json:"conflicts"`
		}{Conflicts: paths})
		return
	}
	if len(paths) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "conflict")
		return
	}
	for _, path := range paths {
		fmt.Fprintf(cmd.ErrOrStderr(), "conflict: %s\n", path)
	}
}
