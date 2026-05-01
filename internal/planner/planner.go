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
		if entry.Type == api.FileTypeFile {
			remotePaths[entry.Path] = true
		}
	}
	deleteOps := remoteDeleteOps(snap, remotePaths, opts)
	ops = append(ops, deleteOps...)
	blockedPrefixes := conflictPrefixSet(deleteOps)

	for _, entry := range manifest.Entries {
		if entry.Type != api.FileTypeFile {
			continue
		}
		if hasConflictAncestor(entry.Path, blockedPrefixes) {
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
	return Plan{Operations: sortedOps(ops)}
}

func PlanPull(manifest api.ManifestResponse, snap state.Snapshot, opts Options) Plan {
	_ = snap
	var ops []Operation
	for _, entry := range manifest.Entries {
		if entry.Type != api.FileTypeFile {
			continue
		}
		if opts.TargetPath != "" && !inTargetScope(entry.Path, opts.TargetPath) {
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
		if !inTargetScope(path, opts.TargetPath) && !isAncestorOfTarget(path, opts.TargetPath) {
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

func conflictPrefixSet(ops []Operation) map[string]bool {
	out := map[string]bool{}
	for _, op := range ops {
		if op.Kind == OpConflict {
			out[strings.Trim(path.Clean(op.Path), "/")] = true
		}
	}
	return out
}

func hasConflictAncestor(filePath string, conflicts map[string]bool) bool {
	cleanPath := strings.Trim(path.Clean(filePath), "/")
	for prefix := range conflicts {
		if prefix != "" && cleanPath != prefix && strings.HasPrefix(cleanPath, prefix+"/") {
			return true
		}
	}
	return false
}

func isCurrent(tracked state.TrackedFile, entry api.FileEntry) bool {
	if tracked.Revision != entry.Revision || tracked.Large != entry.Large {
		return false
	}
	if entry.Hash != "" && tracked.BaseHash != "" && entry.Hash != tracked.BaseHash {
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

func isAncestorOfTarget(filePath, targetPath string) bool {
	if targetPath == "" || targetPath == "." {
		return false
	}
	cleanPath := strings.Trim(path.Clean(filePath), "/")
	target := strings.Trim(path.Clean(targetPath), "/")
	return cleanPath != "" && target != "" && cleanPath != target && strings.HasPrefix(target, cleanPath+"/")
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
