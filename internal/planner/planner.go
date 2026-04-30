package planner

import (
	"path"
	"sort"
	"strings"

	"remork/internal/api"
	"remork/internal/state"
)

type OperationKind string

const (
	OpDownload  OperationKind = "download"
	OpWriteMeta OperationKind = "write_meta"
	OpDelete    OperationKind = "delete"
	OpConflict  OperationKind = "conflict"
	OpSkip      OperationKind = "skip"
)

type Operation struct {
	Path   string
	Kind   OperationKind
	Reason string
	Entry  api.FileEntry
}

type Plan struct {
	Operations []Operation
}

type Options struct {
	WorkspaceRef string
	TargetPath   string
	IncludeLarge bool
	Force        bool
	Dirty        []state.DirtyChange
}

func PlanSync(manifest api.ManifestResponse, snap state.Snapshot, opts Options) Plan {
	dirty := dirtySet(opts.Dirty)
	remotePaths := map[string]bool{}
	var ops []Operation
	for _, entry := range manifest.Entries {
		remotePaths[entry.Path] = true
		if entry.Type != api.FileTypeFile {
			continue
		}
		if dirty[entry.Path] && !opts.Force {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpConflict, Reason: "local dirty", Entry: entry})
			continue
		}
		tracked, exists := snap.Entries[entry.Path]
		if exists && isCurrent(tracked, entry) && !opts.Force {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpSkip, Reason: "current", Entry: entry})
			continue
		}
		if entry.Large {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpWriteMeta, Entry: entry})
		} else {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpDownload, Entry: entry})
		}
	}
	for _, op := range remoteDeleteOps(snap, remotePaths, opts) {
		ops = append(ops, op)
	}
	return Plan{Operations: sortedOps(ops)}
}

func PlanPull(manifest api.ManifestResponse, snap state.Snapshot, opts Options) Plan {
	_ = snap
	var ops []Operation
	for _, entry := range manifest.Entries {
		if entry.Type != api.FileTypeFile {
			continue
		}
		if opts.TargetPath != "" && entry.Path != opts.TargetPath {
			continue
		}
		if entry.Large && !opts.IncludeLarge {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpWriteMeta, Entry: entry})
		} else {
			ops = append(ops, Operation{Path: entry.Path, Kind: OpDownload, Entry: entry})
		}
	}
	return Plan{Operations: sortedOps(ops)}
}

func PlanRemoteDeletes(snap state.Snapshot, opts Options) Plan {
	return Plan{Operations: sortedOps(remoteDeleteOps(snap, map[string]bool{}, opts))}
}

func remoteDeleteOps(snap state.Snapshot, remotePaths map[string]bool, opts Options) []Operation {
	dirty := dirtySet(opts.Dirty)
	var ops []Operation
	for path, tracked := range snap.Entries {
		if tracked.Path == "" || remotePaths[path] {
			continue
		}
		if !inTargetScope(path, opts.TargetPath) {
			continue
		}
		if dirty[path] && !opts.Force {
			ops = append(ops, Operation{Path: path, Kind: OpConflict, Reason: "remote deleted local dirty file"})
		} else {
			ops = append(ops, Operation{Path: path, Kind: OpDelete, Reason: "remote deleted file"})
		}
	}
	return ops
}

func isCurrent(tracked state.TrackedFile, entry api.FileEntry) bool {
	if tracked.Revision != entry.Revision || tracked.Large != entry.Large {
		return false
	}
	if entry.Large && tracked.MetaPath == "" {
		return false
	}
	return true
}

func inTargetScope(filePath, targetPath string) bool {
	if targetPath == "" || targetPath == "." {
		return true
	}
	target := strings.Trim(path.Clean(targetPath), "/")
	if target == "" || target == "." {
		return true
	}
	cleanPath := strings.Trim(path.Clean(filePath), "/")
	return cleanPath == target || strings.HasPrefix(cleanPath, target+"/")
}

func dirtySet(changes []state.DirtyChange) map[string]bool {
	out := map[string]bool{}
	for _, c := range changes {
		out[c.Path] = true
	}
	return out
}

func sortedOps(ops []Operation) []Operation {
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path == ops[j].Path {
			return ops[i].Kind < ops[j].Kind
		}
		return ops[i].Path < ops[j].Path
	})
	return ops
}
