package syncer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"remork/internal/api"
	"remork/internal/client"
	"remork/internal/manifest"
	"remork/internal/planner"
	"remork/internal/state"
	"remork/internal/transfer"
)

type ProgressReporter interface {
	Start(label string, total int64)
	Advance(delta int64)
	Done()
}

type RunnerOptions struct {
	Client       client.Client
	StateStore   state.Store
	LocalRoot    string
	WorkspaceRef string
	RemoteRoot   string
	Progress     ProgressReporter
}

type SyncOptions struct {
	TargetPath   string
	IncludeLarge bool
	Force        bool
	Quiet        bool
}

type Result struct {
	Downloaded  int `json:"downloaded"`
	MetaWritten int `json:"meta_written"`
	Deleted     int `json:"deleted"`
	Skipped     int `json:"skipped"`
	Conflicts   int `json:"conflicts"`
}

type Status struct {
	Workspace         string   `json:"workspace"`
	LocalRoot         string   `json:"local_root"`
	Clean             int      `json:"clean"`
	LocalChanges      int      `json:"local_changes"`
	RemoteUpdates     int      `json:"remote_updates"`
	Conflicts         int      `json:"conflicts"`
	LargePlaceholders int      `json:"large_placeholders"`
	ChangedPaths      []string `json:"changed_paths"`
	ConflictPaths     []string `json:"conflict_paths"`
}

type Runner struct {
	opts RunnerOptions
}

func NewRunner(opts RunnerOptions) Runner {
	return Runner{opts: opts}
}

func (r Runner) Status(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	snap, err := r.loadSnapshot()
	if err != nil {
		return Status{}, err
	}
	dirty, err := state.DetectDirty(r.opts.LocalRoot, snap)
	if err != nil {
		return Status{}, err
	}
	man, err := r.opts.Client.Manifest(r.opts.RemoteRoot, ".")
	if err != nil {
		return Status{}, err
	}

	dirtyPaths := map[string]bool{}
	changedPaths := make([]string, 0, len(dirty))
	for _, change := range dirty {
		if isLocalOnlyPath(change.Path) {
			continue
		}
		if !dirtyPaths[change.Path] {
			changedPaths = append(changedPaths, change.Path)
		}
		dirtyPaths[change.Path] = true
	}
	sort.Strings(changedPaths)

	remoteUpdates := map[string]bool{}
	remotePaths := map[string]bool{}
	for _, entry := range man.Entries {
		if isLocalOnlyPath(entry.Path) {
			continue
		}
		remotePaths[entry.Path] = true
		if entry.Type != api.FileTypeFile {
			continue
		}
		tracked, ok := snap.Entries[entry.Path]
		if !ok || !statusCurrent(tracked, entry) {
			remoteUpdates[entry.Path] = true
		}
	}
	for path, tracked := range snap.Entries {
		if tracked.Path == "" || remotePaths[path] || isLocalOnlyPath(path) {
			continue
		}
		remoteUpdates[path] = true
	}

	conflictPaths := make([]string, 0)
	for path := range dirtyPaths {
		if remoteUpdates[path] {
			conflictPaths = append(conflictPaths, path)
		}
	}
	sort.Strings(conflictPaths)

	clean := 0
	for path := range snap.Entries {
		if isLocalOnlyPath(path) {
			continue
		}
		if !dirtyPaths[path] && !remoteUpdates[path] {
			clean++
		}
	}

	largePlaceholders := 0
	for _, tracked := range snap.Entries {
		if isLocalOnlyPath(tracked.Path) || !tracked.Large || tracked.MetaPath == "" {
			continue
		}
		metaPath, err := transfer.LocalPath(r.opts.LocalRoot, tracked.MetaPath)
		if err != nil {
			return Status{}, err
		}
		if _, err := os.Stat(metaPath); err == nil {
			largePlaceholders++
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Status{}, err
		}
	}

	return Status{
		Workspace:         r.opts.WorkspaceRef,
		LocalRoot:         r.opts.LocalRoot,
		Clean:             clean,
		LocalChanges:      len(changedPaths),
		RemoteUpdates:     len(remoteUpdates),
		Conflicts:         len(conflictPaths),
		LargePlaceholders: largePlaceholders,
		ChangedPaths:      changedPaths,
		ConflictPaths:     conflictPaths,
	}, nil
}

func (r Runner) Sync(ctx context.Context, opts SyncOptions) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	snap, err := r.loadSnapshot()
	if err != nil {
		return Result{}, err
	}

	dirty, err := state.DetectDirty(r.opts.LocalRoot, snap)
	if err != nil {
		return Result{}, err
	}
	dirty = filterDirtyLocalOnly(dirty)
	target := opts.TargetPath
	if target == "" {
		target = "."
	}
	man, err := r.opts.Client.Manifest(r.opts.RemoteRoot, target)
	if err != nil {
		return Result{}, err
	}
	plan := planner.PlanSync(filterManifestLocalOnly(man), filterSnapshotLocalOnly(snap), planner.Options{
		WorkspaceRef: r.opts.WorkspaceRef,
		TargetPath:   opts.TargetPath,
		IncludeLarge: opts.IncludeLarge,
		Force:        opts.Force,
		Dirty:        dirty,
	})

	var result Result
	for _, op := range plan.Operations {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		switch op.Kind {
		case planner.OpDownload:
			previous := snap.Entries[op.Path]
			data, err := r.opts.Client.Download(r.opts.RemoteRoot, op.Path)
			if err != nil {
				return result, err
			}
			if err := transfer.WriteFile(r.opts.LocalRoot, op.Path, data); err != nil {
				return result, err
			}
			basePath, err := r.opts.StateStore.BasePath(op.Path)
			if err != nil {
				return result, err
			}
			if err := writeExactFile(basePath, data); err != nil {
				return result, err
			}
			if previous.Large && previous.MetaPath != "" {
				metaPath, err := transfer.LocalPath(r.opts.LocalRoot, previous.MetaPath)
				if err != nil {
					return result, err
				}
				if err := removeIfExists(metaPath); err != nil {
					return result, err
				}
			}
			hash := op.Entry.Hash
			if hash == "" {
				hash = state.HashBytes(data)
			}
			snap.Entries[op.Path] = state.TrackedFile{
				Path:     op.Path,
				Type:     op.Entry.Type,
				BaseHash: hash,
				Revision: op.Entry.Revision,
				Large:    false,
			}
			result.Downloaded++
		case planner.OpWriteMeta:
			meta := manifest.BuildLargeMeta(r.opts.WorkspaceRef, op.Entry)
			if err := transfer.WriteLargeMeta(r.opts.LocalRoot, op.Path, meta); err != nil {
				return result, err
			}
			localPath, err := transfer.LocalPath(r.opts.LocalRoot, op.Path)
			if err != nil {
				return result, err
			}
			if err := removeIfExists(localPath); err != nil {
				return result, err
			}
			snap.Entries[op.Path] = state.TrackedFile{
				Path:     op.Path,
				MetaPath: op.Path + ".meta",
				Type:     op.Entry.Type,
				BaseHash: op.Entry.Hash,
				Revision: op.Entry.Revision,
				Large:    true,
			}
			result.MetaWritten++
		case planner.OpDelete:
			localPath, err := transfer.LocalPath(r.opts.LocalRoot, op.Path)
			if err != nil {
				return result, err
			}
			if err := removeIfExists(localPath); err != nil {
				return result, err
			}
			if tracked, ok := snap.Entries[op.Path]; ok && tracked.MetaPath != "" {
				metaPath, err := transfer.LocalPath(r.opts.LocalRoot, tracked.MetaPath)
				if err != nil {
					return result, err
				}
				if err := removeIfExists(metaPath); err != nil {
					return result, err
				}
			}
			delete(snap.Entries, op.Path)
			result.Deleted++
		case planner.OpConflict:
			result.Conflicts++
		case planner.OpSkip:
			result.Skipped++
		}
	}
	if err := r.opts.StateStore.Save(snap); err != nil {
		return result, err
	}
	return result, nil
}

func (r Runner) loadSnapshot() (state.Snapshot, error) {
	snap, err := r.opts.StateStore.Load(r.opts.WorkspaceRef)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return state.Snapshot{}, err
		}
		snap = state.Snapshot{WorkspaceRef: r.opts.WorkspaceRef, Entries: map[string]state.TrackedFile{}}
	}
	if snap.WorkspaceRef == "" {
		snap.WorkspaceRef = r.opts.WorkspaceRef
	}
	if snap.Entries == nil {
		snap.Entries = map[string]state.TrackedFile{}
	}
	return snap, nil
}

func statusCurrent(tracked state.TrackedFile, entry api.FileEntry) bool {
	if tracked.Revision != entry.Revision || tracked.Large != entry.Large {
		return false
	}
	if entry.Large && tracked.MetaPath == "" {
		return false
	}
	return true
}

func isLocalOnlyPath(path string) bool {
	return path == ".remork-local.json"
}

func filterManifestLocalOnly(man api.ManifestResponse) api.ManifestResponse {
	filtered := man
	filtered.Entries = make([]api.FileEntry, 0, len(man.Entries))
	for _, entry := range man.Entries {
		if isLocalOnlyPath(entry.Path) {
			continue
		}
		filtered.Entries = append(filtered.Entries, entry)
	}
	return filtered
}

func filterSnapshotLocalOnly(snap state.Snapshot) state.Snapshot {
	filtered := snap
	filtered.Entries = make(map[string]state.TrackedFile, len(snap.Entries))
	for path, tracked := range snap.Entries {
		if isLocalOnlyPath(path) || isLocalOnlyPath(tracked.Path) {
			continue
		}
		filtered.Entries[path] = tracked
	}
	return filtered
}

func filterDirtyLocalOnly(dirty []state.DirtyChange) []state.DirtyChange {
	filtered := make([]state.DirtyChange, 0, len(dirty))
	for _, change := range dirty {
		if isLocalOnlyPath(change.Path) {
			continue
		}
		filtered = append(filtered, change)
	}
	return filtered
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeExactFile(path string, data []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".remork-base-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
