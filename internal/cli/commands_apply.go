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
	"remork/internal/prompt"
	"remork/internal/state"
	"remork/internal/syncer"
	"remork/internal/transfer"
	"remork/internal/workspace"
)

type codedCommandError struct {
	code int
	err  error
	fix  string
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

func (e codedCommandError) Fix() string {
	return e.fix
}

func addApplyCommand(root *cobra.Command, opts Options) {
	var dryRun bool
	var jsonOut bool
	var includeUntracked bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "apply [path...]",
		Short: "Write local changes to the remote after base checks",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateWorkspacePathArgs(args); err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			runner, binding, localRoot, workspaceRef, err := boundApplyContext(opts)
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			snap, err := state.NewStore(binding.StateDir).Load(workspaceRef)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				snap = state.Snapshot{WorkspaceRef: workspaceRef, Entries: map[string]state.TrackedFile{}}
			}
			changeset, skipped, err := syncer.BuildChangesetWithOptions(localRoot, snap, syncer.BuildChangesetOptions{
				UseIgnoreFiles:   true,
				IncludeUntracked: includeUntracked,
				ExplicitPaths:    args,
			})
			if err != nil {
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
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
						Skipped: normalizeSkipped(skipped),
						DryRun:  true,
					})
				}
				return nil
			}
			if len(changeset.Changes) == 0 && len(skipped) > 0 && !jsonOut {
				plainRenderer(cmd, false).Success("applied 0")
				plainErrRenderer(cmd, false).Warning("Skipped untracked or ignored files. Use remork apply <path> or --include-untracked when you intend to create remote files.")
				return nil
			}
			if len(changeset.Changes) == 0 {
				if jsonOut {
					return output.WriteJSON(cmd.OutOrStdout(), applyJSONResult{
						ID:      changeset.ID,
						Plan:    summary,
						Skipped: normalizeSkipped(skipped),
						Applied: 0,
					})
				}
				if !jsonOut {
					r := plainRenderer(cmd, false)
					r.Section("Apply complete")
					r.Success("applied 0")
				}
				return nil
			}
			mode := commandInteractionMode(cmd, interactionRequest{JSON: jsonOut, Yes: yes})
			if !yes && !mode.RichOutput {
				err := applyRequiresYesError()
				if jsonOut {
					return writeJSONCommandError(cmd, err)
				}
				return err
			}
			if mode.RichOutput && !yes {
				ok, err := prompt.Confirm(prompt.Options{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr()}, fmt.Sprintf("apply %d change(s) to the remote?", len(changeset.Changes)))
				if err != nil {
					return err
				}
				if !ok {
					plainRenderer(cmd, false).Warning("apply cancelled")
					return nil
				}
			}
			ctx := cmd.Context()
			result, err := runner.ApplyChangesetContext(ctx, changeset)
			if err != nil {
				if isApplyConflict(err, result) {
					writeApplyConflict(cmd, result.Conflicts, jsonOut)
					return silentCommandError{err: applyConflictError(result.Conflicts)}
				}
				if isApplyPartialFailure(result) {
					writeApplyPartialFailure(cmd, result, jsonOut)
					return applyPartialError(result)
				}
				return err
			}
			if isApplyPartialFailure(result) {
				writeApplyPartialFailure(cmd, result, jsonOut)
				return applyPartialError(result)
			}
			if !result.Applied {
				writeApplyConflict(cmd, result.Conflicts, jsonOut)
				return silentCommandError{err: applyConflictError(result.Conflicts)}
			}
			if err := refreshAppliedChanges(ctx, runner, binding, localRoot, workspaceRef, changeset.Changes); err != nil {
				return err
			}
			if jsonOut {
				return output.WriteJSON(cmd.OutOrStdout(), applyJSONResult{
					ID:      changeset.ID,
					Plan:    summary,
					Skipped: normalizeSkipped(skipped),
					Applied: len(changeset.Changes),
				})
			}
			if !jsonOut {
				r := plainRenderer(cmd, false)
				r.Section("Apply complete")
				r.KeyValue("applied", len(changeset.Changes))
				r.Success(fmt.Sprintf("applied %d", len(changeset.Changes)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the apply plan without writing remote files")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&includeUntracked, "include-untracked", false, "Include untracked local files in the apply changeset")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm apply without prompting")
	root.AddCommand(cmd)
}

type applyJSONResult struct {
	ID      string                 `json:"id"`
	Plan    map[string]int         `json:"plan"`
	Skipped []syncer.SkippedChange `json:"skipped"`
	DryRun  bool                   `json:"dry_run,omitempty"`
	Applied int                    `json:"applied,omitempty"`
}

func normalizeSkipped(skipped []syncer.SkippedChange) []syncer.SkippedChange {
	if skipped == nil {
		return []syncer.SkippedChange{}
	}
	return skipped
}

func applyRequiresYesError() error {
	return codedCommandError{
		code: exitcode.PromptRequired,
		err:  errors.New("apply requires --yes in non-interactive, JSON, or non-TTY mode"),
		fix:  "review remork diff, then rerun remork apply --yes",
	}
}

func boundApplyContext(opts Options) (syncer.Runner, workspace.Binding, string, string, error) {
	binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return syncer.Runner{}, workspace.Binding{}, "", "", unboundWorkspaceError(err)
	}
	runner, err := newBoundSyncRunner(opts)
	if err != nil {
		return syncer.Runner{}, workspace.Binding{}, "", "", err
	}
	return runner, binding, localRoot, binding.Host + ":" + binding.RemoteRoot, nil
}

func refreshAppliedChanges(ctx context.Context, runner syncer.Runner, binding workspace.Binding, localRoot, workspaceRef string, changes []apply.Change) error {
	seen := map[string]bool{}
	var deletes []apply.Change
	for _, change := range changes {
		if change.Kind == apply.ChangeDelete {
			deletes = append(deletes, change)
			continue
		}
		if seen[change.Path] {
			continue
		}
		seen[change.Path] = true
		if _, err := runner.Sync(ctx, syncer.SyncOptions{Force: true, TargetPath: change.Path}); err != nil {
			return err
		}
	}
	if len(deletes) == 0 {
		return nil
	}
	return forgetAppliedDeletes(binding, localRoot, workspaceRef, deletes)
}

func forgetAppliedDeletes(binding workspace.Binding, localRoot, workspaceRef string, changes []apply.Change) error {
	store := state.NewStore(binding.StateDir)
	snap, err := store.Load(workspaceRef)
	if err != nil {
		return err
	}
	for _, change := range changes {
		tracked := snap.Entries[change.Path]
		basePath, err := store.BasePath(change.Path)
		if err != nil {
			return err
		}
		if err := removeFileIfExists(basePath); err != nil {
			return err
		}
		if tracked.MetaPath != "" {
			metaPath, err := transfer.LocalPath(localRoot, tracked.MetaPath)
			if err != nil {
				return err
			}
			if err := removeFileIfExists(metaPath); err != nil {
				return err
			}
		}
		delete(snap.Entries, change.Path)
	}
	return store.Save(snap)
}

func removeFileIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
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
	r := plainRenderer(cmd, false)
	r.Section("Apply plan")
	r.KeyValue("create", summary["create"])
	r.KeyValue("update", summary["update"])
	r.KeyValue("delete", summary["delete"])
	r.KeyValue("skipped", summary["skipped"])
}

func isApplyConflict(err error, result apply.Result) bool {
	var httpErr *client.HTTPError
	return len(result.Conflicts) > 0 || errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict
}

func isApplyPartialFailure(result apply.Result) bool {
	return result.FailedPath != "" || len(result.Partial) > 0
}

func applyConflictError(paths []string) error {
	if len(paths) == 0 {
		return codedCommandError{code: exitcode.Conflict, err: errors.New("conflict"), fix: "run remork status, inspect conflicts, then run remork sync before retrying"}
	}
	return codedCommandError{code: exitcode.Conflict, err: fmt.Errorf("conflict: %v", paths), fix: "run remork conflict -- <path> for each conflict, then run remork sync before retrying"}
}

func applyPartialError(result apply.Result) error {
	if result.FailedPath == "" {
		return errors.New("apply failed after partial mutation")
	}
	return fmt.Errorf("apply failed at %s after partial mutation", result.FailedPath)
}

func writeApplyConflict(cmd *cobra.Command, paths []string, jsonOut bool) {
	if jsonOut {
		_ = output.WriteJSON(cmd.OutOrStdout(), struct {
			Error     string   `json:"error"`
			Fix       string   `json:"fix"`
			Code      int      `json:"code"`
			Conflicts []string `json:"conflicts"`
		}{
			Error:     applyConflictError(paths).Error(),
			Fix:       commandErrorFix(applyConflictError(paths)),
			Code:      commandErrorExitCode(applyConflictError(paths)),
			Conflicts: paths,
		})
		return
	}
	if len(paths) == 0 {
		plainErrRenderer(cmd, false).Error("conflict", "run remork status")
		return
	}
	r := plainErrRenderer(cmd, false)
	r.Section("Apply conflict")
	for _, path := range paths {
		r.Error("conflict: "+path, "inspect: "+pathCommand("conflict", path))
	}
}

func writeApplyPartialFailure(cmd *cobra.Command, result apply.Result, jsonOut bool) {
	if jsonOut {
		_ = output.WriteJSON(cmd.OutOrStdout(), result)
		return
	}
	r := plainErrRenderer(cmd, false)
	r.Section("Apply failed")
	if result.FailedPath != "" {
		r.Error("apply failed at: "+result.FailedPath, "Run remork status and remork sync before retrying.")
	} else {
		r.Error("apply failed after partial mutation", "Run remork status and remork sync before retrying.")
	}
	if len(result.Partial) == 0 {
		r.KeyValue("changed paths", "none")
	} else {
		r.KeyValue("changed paths", result.Partial)
	}
}
