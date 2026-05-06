package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	diffpkg "remork/internal/diff"
	"remork/internal/state"
	"remork/internal/transfer"
	"remork/internal/workspace"
)

func addDiffCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "diff [path]",
		Short: "Show local changes against the synced base",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := loadLocalChangeContext(opts)
			if err != nil {
				return err
			}
			changes, err := state.DetectDirty(ctx.localRoot, ctx.snapshot)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				changes = filterChanges(changes, args[0])
			}
			for _, change := range changes {
				tracked := ctx.snapshot.Entries[change.Path]
				text, err := renderChangeDiff(ctx, change, tracked)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), text)
			}
			return nil
		},
	}
	root.AddCommand(cmd)
}

type localChangeContext struct {
	localRoot string
	store     state.Store
	snapshot  state.Snapshot
}

func loadLocalChangeContext(opts Options) (localChangeContext, error) {
	binding, localRoot, err := workspace.ResolveFrom(opts.WorkingDir)
	if err != nil {
		return localChangeContext{}, unboundWorkspaceError(err)
	}
	store := state.NewStore(binding.StateDir)
	workspaceRef := binding.Host + ":" + binding.RemoteRoot
	snap, err := store.Load(workspaceRef)
	if err != nil {
		return localChangeContext{}, err
	}
	return localChangeContext{localRoot: localRoot, store: store, snapshot: snap}, nil
}

func renderChangeDiff(ctx localChangeContext, change state.DirtyChange, tracked state.TrackedFile) (string, error) {
	switch change.Kind {
	case state.ChangeCreate:
		localPath, err := transfer.LocalPath(ctx.localRoot, change.Path)
		if err != nil {
			return "", err
		}
		newData, err := os.ReadFile(localPath)
		if err != nil {
			return "", err
		}
		if containsNUL(newData) {
			return diffpkg.Metadata(change.Path, diffpkg.MetadataChange{NewSize: int64(len(newData))}), nil
		}
		return diffpkg.UnifiedText(change.Path, nil, newData), nil
	case state.ChangeModify, state.ChangeDelete:
		baseData, err := readBase(ctx.store, change.Path)
		if err != nil {
			return "", err
		}
		var newData []byte
		if change.Kind == state.ChangeModify {
			localPath, err := transfer.LocalPath(ctx.localRoot, change.Path)
			if err != nil {
				return "", err
			}
			newData, err = os.ReadFile(localPath)
			if err != nil {
				return "", err
			}
		}
		if tracked.Large || containsNUL(baseData) || containsNUL(newData) {
			return diffpkg.Metadata(change.Path, diffpkg.MetadataChange{
				OldSize: int64(len(baseData)),
				NewSize: int64(len(newData)),
				Large:   tracked.Large,
			}), nil
		}
		return diffpkg.UnifiedText(change.Path, baseData, newData), nil
	default:
		return "", nil
	}
}

func readBase(store state.Store, remotePath string) ([]byte, error) {
	basePath, err := store.BasePath(remotePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(basePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("base cache for %q is missing; run remork sync --force", remotePath)
		}
		return nil, err
	}
	return data, nil
}

func filterChanges(changes []state.DirtyChange, path string) []state.DirtyChange {
	filtered := make([]state.DirtyChange, 0, len(changes))
	for _, change := range changes {
		if change.Path == path {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

func containsNUL(data []byte) bool {
	return bytes.Contains(data, []byte{0})
}
