package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"remork/internal/api"
)

type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeModify ChangeKind = "modify"
	ChangeDelete ChangeKind = "delete"
)

type TrackedFile struct {
	Path     string       `json:"path"`
	MetaPath string       `json:"meta_path,omitempty"`
	Type     api.FileType `json:"type"`
	BaseHash string       `json:"base_hash,omitempty"`
	Revision string       `json:"revision"`
	Large    bool         `json:"large"`
}

type Snapshot struct {
	WorkspaceRef string                 `json:"workspace_ref"`
	Entries      map[string]TrackedFile `json:"entries"`
}

type DirtyChange struct {
	Path string     `json:"path"`
	Kind ChangeKind `json:"kind"`
}

type Store struct {
	dir string
}

func NewStore(dir string) Store {
	return Store{dir: dir}
}

func (s Store) Save(snapshot Snapshot) error {
	if snapshot.Entries == nil {
		snapshot.Entries = map[string]TrackedFile{}
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(snapshot.WorkspaceRef)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s Store) Load(workspaceRef string) (Snapshot, error) {
	data, err := os.ReadFile(s.path(workspaceRef))
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, err
	}
	if snap.Entries == nil {
		snap.Entries = map[string]TrackedFile{}
	}
	return snap, nil
}

func (s Store) path(workspaceRef string) string {
	name := strings.NewReplacer("/", "_", ":", "_").Replace(workspaceRef)
	return filepath.Join(s.dir, name+".json")
}

func DetectDirty(localRoot string, snap Snapshot) ([]DirtyChange, error) {
	var changes []DirtyChange
	seen := map[string]bool{}
	for path, tracked := range snap.Entries {
		if tracked.Large && tracked.MetaPath != "" {
			continue
		}
		seen[path] = true
		full := filepath.Join(localRoot, filepath.FromSlash(path))
		info, err := os.Stat(full)
		if os.IsNotExist(err) {
			changes = append(changes, DirtyChange{Path: path, Kind: ChangeDelete})
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		hash, err := HashFile(full)
		if err != nil {
			return nil, err
		}
		if tracked.BaseHash != "" && hash != tracked.BaseHash {
			changes = append(changes, DirtyChange{Path: path, Kind: ChangeModify})
		}
	}
	err := filepath.WalkDir(localRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".remork" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(localRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".meta") {
			return nil
		}
		if !seen[rel] {
			changes = append(changes, DirtyChange{Path: rel, Kind: ChangeCreate})
		}
		return nil
	})
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path == changes[j].Path {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].Path < changes[j].Path
	})
	return changes, err
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
