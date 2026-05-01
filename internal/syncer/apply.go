package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"remork/internal/api"
	"remork/internal/apply"
	"remork/internal/paths"
	"remork/internal/state"
	"remork/internal/transfer"
)

type SkippedChange struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type BuildChangesetOptions struct {
	UseIgnoreFiles   bool
	IncludeUntracked bool
	ExplicitPaths    []string
}

func (r Runner) ApplyChangeset(cs apply.Changeset) (apply.Result, error) {
	return r.ApplyChangesetContext(context.Background(), cs)
}

func (r Runner) ApplyChangesetContext(ctx context.Context, cs apply.Changeset) (apply.Result, error) {
	return r.opts.Client.ApplyContext(ctx, r.opts.RemoteRoot, cs)
}

func BuildChangeset(localRoot string, snap state.Snapshot) (apply.Changeset, []SkippedChange, error) {
	return BuildChangesetWithOptions(localRoot, snap, BuildChangesetOptions{UseIgnoreFiles: true})
}

func BuildChangesetWithOptions(localRoot string, snap state.Snapshot, opts BuildChangesetOptions) (apply.Changeset, []SkippedChange, error) {
	dirty, err := state.DetectDirtyWithOptions(localRoot, snap, state.DirtyOptions{UseIgnoreFiles: opts.UseIgnoreFiles})
	if err != nil {
		return apply.Changeset{}, nil, err
	}
	skipped, err := skippedPlaceholderChanges(localRoot, snap)
	if err != nil {
		return apply.Changeset{}, nil, err
	}
	explicit, err := normalizeExplicitPaths(localRoot, opts.ExplicitPaths)
	if err != nil {
		return apply.Changeset{}, nil, err
	}

	changes := make([]apply.Change, 0, len(dirty))
	for _, change := range dirty {
		if skip, reason := shouldSkipApplyPath(change.Path); skip {
			skipped = append(skipped, SkippedChange{Path: change.Path, Reason: reason})
			continue
		}
		if _, err := paths.NormalizeRemotePath(change.Path); err != nil {
			return apply.Changeset{}, nil, err
		}
		if explicit.filters() && !explicit.selects(change.Path) {
			if change.Kind == state.ChangeCreate {
				skipped = append(skipped, SkippedChange{Path: change.Path, Reason: "untracked local file; pass --include-untracked or an explicit path"})
			}
			continue
		}
		tracked := snap.Entries[change.Path]
		switch change.Kind {
		case state.ChangeCreate:
			if !opts.IncludeUntracked && !explicit.selects(change.Path) {
				skipped = append(skipped, SkippedChange{Path: change.Path, Reason: "untracked local file; pass --include-untracked or an explicit path"})
				continue
			}
			localPath, err := transfer.LocalPath(localRoot, change.Path)
			if err != nil {
				return apply.Changeset{}, nil, err
			}
			data, err := os.ReadFile(localPath)
			if err != nil {
				return apply.Changeset{}, nil, err
			}
			changes = append(changes, apply.Change{Path: change.Path, Kind: apply.ChangeCreate, Content: data})
		case state.ChangeModify:
			localPath, err := transfer.LocalPath(localRoot, change.Path)
			if err != nil {
				return apply.Changeset{}, nil, err
			}
			data, err := os.ReadFile(localPath)
			if err != nil {
				return apply.Changeset{}, nil, err
			}
			changes = append(changes, apply.Change{Path: change.Path, Kind: apply.ChangeUpdate, BaseHash: tracked.BaseHash, Content: data})
		case state.ChangeDelete:
			changes = append(changes, apply.Change{Path: change.Path, Kind: apply.ChangeDelete, BaseHash: tracked.BaseHash})
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path == changes[j].Path {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].Path < changes[j].Path
	})
	sort.Slice(skipped, func(i, j int) bool {
		return skipped[i].Path < skipped[j].Path
	})
	return apply.Changeset{ID: changesetID(changes, skipped), Changes: changes}, skipped, nil
}

type explicitPathSet []string

func normalizeExplicitPaths(localRoot string, values []string) (explicitPathSet, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make(explicitPathSet, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(value)
		clean, err := paths.NormalizeRemotePath(value)
		if err != nil {
			return nil, err
		}
		if _, err := transfer.LocalPath(localRoot, clean); err != nil {
			return nil, err
		}
		normalized = append(normalized, clean)
	}
	return normalized, nil
}

func (paths explicitPathSet) selects(path string) bool {
	for _, explicit := range paths {
		if path == explicit || strings.HasPrefix(path, explicit+"/") {
			return true
		}
	}
	return false
}

func (paths explicitPathSet) filters() bool {
	return len(paths) > 0
}

func skippedPlaceholderChanges(localRoot string, snap state.Snapshot) ([]SkippedChange, error) {
	var skipped []SkippedChange
	for _, tracked := range snap.Entries {
		if !tracked.Large || tracked.MetaPath == "" {
			continue
		}
		metaPath, err := transfer.LocalPath(localRoot, tracked.MetaPath)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(metaPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !largeMetaMatchesSnapshot(data, snap.WorkspaceRef, tracked) {
			skipped = append(skipped, SkippedChange{Path: tracked.MetaPath, Reason: "large file placeholder"})
		}
	}
	return skipped, nil
}

func largeMetaMatchesSnapshot(data []byte, workspaceRef string, tracked state.TrackedFile) bool {
	var meta api.LargeFileMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}
	if meta.Kind != "remote-large-file" || meta.RemotePath != "/"+strings.TrimPrefix(tracked.Path, "/") || meta.Pulled {
		return false
	}
	if tracked.Revision != "" && meta.Revision != tracked.Revision {
		return false
	}
	if tracked.BaseHash != "" && meta.Hash != tracked.BaseHash {
		return false
	}
	if workspaceRef != "" {
		wantPull := "remork pull " + strings.TrimRight(workspaceRef, "/") + "/" + tracked.Path
		if meta.PullCommand != wantPull {
			return false
		}
	}
	return true
}

func shouldSkipApplyPath(path string) (bool, string) {
	if path == ".remork-local.json" {
		return true, "local binding marker"
	}
	if path == ".git" || strings.HasPrefix(path, ".git/") {
		return true, "git metadata"
	}
	if path == ".remork" || strings.HasPrefix(path, ".remork/") {
		return true, "remork metadata"
	}
	if strings.HasSuffix(path, ".meta") {
		return true, "large file placeholder"
	}
	return false, ""
}

func changesetID(changes []apply.Change, skipped []SkippedChange) string {
	h := sha256.New()
	for _, change := range changes {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", change.Path, change.Kind, change.BaseHash, state.HashBytes(change.Content))
	}
	for _, skip := range skipped {
		fmt.Fprintf(h, "skip\x00%s\x00%s\x00", skip.Path, skip.Reason)
	}
	return "cs-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(h.Sum(nil))[:12]
}
