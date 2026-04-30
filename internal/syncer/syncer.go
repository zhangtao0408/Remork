package syncer

import (
	"context"
	"errors"
	"os"

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

type Runner struct {
	opts RunnerOptions
}

func NewRunner(opts RunnerOptions) Runner {
	return Runner{opts: opts}
}

func (r Runner) Sync(ctx context.Context, opts SyncOptions) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	snap, err := r.opts.StateStore.Load(r.opts.WorkspaceRef)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Result{}, err
		}
		snap = state.Snapshot{WorkspaceRef: r.opts.WorkspaceRef, Entries: map[string]state.TrackedFile{}}
	}
	if snap.WorkspaceRef == "" {
		snap.WorkspaceRef = r.opts.WorkspaceRef
	}
	if snap.Entries == nil {
		snap.Entries = map[string]state.TrackedFile{}
	}

	dirty, err := state.DetectDirty(r.opts.LocalRoot, snap)
	if err != nil {
		return Result{}, err
	}
	target := opts.TargetPath
	if target == "" {
		target = "."
	}
	man, err := r.opts.Client.Manifest(r.opts.RemoteRoot, target)
	if err != nil {
		return Result{}, err
	}
	plan := planner.PlanSync(man, snap, planner.Options{
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
			data, err := r.opts.Client.Download(r.opts.RemoteRoot, op.Path)
			if err != nil {
				return result, err
			}
			if err := transfer.WriteFile(r.opts.LocalRoot, op.Path, data); err != nil {
				return result, err
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

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
