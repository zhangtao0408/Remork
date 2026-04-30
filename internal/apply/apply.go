package apply

import (
	"errors"
	"os"
	"path/filepath"

	"remork/internal/paths"
	"remork/internal/state"
)

type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeDelete ChangeKind = "delete"
)

type Change struct {
	Path     string     `json:"path"`
	Kind     ChangeKind `json:"kind"`
	BaseHash string     `json:"base_hash,omitempty"`
	Content  []byte     `json:"content,omitempty"`
}

type Changeset struct {
	ID      string   `json:"id,omitempty"`
	Changes []Change `json:"changes"`
}

type Result struct {
	Applied   bool     `json:"applied"`
	Conflicts []string `json:"conflicts,omitempty"`
}

var ErrConflict = errors.New("apply conflict")

func Apply(root string, cs Changeset) (Result, error) {
	conflicts, err := verify(root, cs)
	if err != nil {
		return Result{Applied: false}, err
	}
	if len(conflicts) > 0 {
		return Result{Applied: false, Conflicts: conflicts}, ErrConflict
	}
	for _, ch := range cs.Changes {
		full, err := paths.ResolveInsideWorkspace(root, ch.Path)
		if err != nil {
			return Result{Applied: false}, err
		}
		switch ch.Kind {
		case ChangeCreate, ChangeUpdate:
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return Result{Applied: false}, err
			}
			tmp := full + ".remork-apply"
			if err := os.WriteFile(tmp, ch.Content, 0o644); err != nil {
				return Result{Applied: false}, err
			}
			if err := os.Rename(tmp, full); err != nil {
				return Result{Applied: false}, err
			}
		case ChangeDelete:
			if err := os.Remove(full); err != nil {
				return Result{Applied: false}, err
			}
		default:
			return Result{Applied: false}, errors.New("unknown change kind")
		}
	}
	return Result{Applied: true}, nil
}

func verify(root string, cs Changeset) ([]string, error) {
	var conflicts []string
	for _, ch := range cs.Changes {
		full, err := paths.ResolveInsideWorkspace(root, ch.Path)
		if err != nil {
			return nil, err
		}
		switch ch.Kind {
		case ChangeCreate:
			if _, err := os.Stat(full); err == nil {
				conflicts = append(conflicts, ch.Path)
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		case ChangeUpdate, ChangeDelete:
			hash, err := state.HashFile(full)
			if err != nil || hash != ch.BaseHash {
				conflicts = append(conflicts, ch.Path)
			}
		default:
			return nil, errors.New("unknown change kind")
		}
	}
	return conflicts, nil
}
