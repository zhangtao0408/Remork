package syncer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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

func (r Runner) ApplyChangeset(cs apply.Changeset) (apply.Result, error) {
	return r.opts.Client.Apply(r.opts.RemoteRoot, cs)
}

func BuildChangeset(localRoot string, snap state.Snapshot) (apply.Changeset, []SkippedChange, error) {
	dirty, err := state.DetectDirty(localRoot, snap)
	if err != nil {
		return apply.Changeset{}, nil, err
	}
	skipped, err := skippedPlaceholderChanges(localRoot, snap)
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
		localPath, err := transfer.LocalPath(localRoot, change.Path)
		if err != nil {
			return apply.Changeset{}, nil, err
		}
		tracked := snap.Entries[change.Path]
		switch change.Kind {
		case state.ChangeCreate:
			data, err := os.ReadFile(localPath)
			if err != nil {
				return apply.Changeset{}, nil, err
			}
			changes = append(changes, apply.Change{Path: change.Path, Kind: apply.ChangeCreate, Content: data})
		case state.ChangeModify:
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
