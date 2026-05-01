package apply

import (
	"errors"
	"os"
	"path/filepath"

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
	Applied    bool     `json:"applied"`
	Conflicts  []string `json:"conflicts,omitempty"`
	Partial    []string `json:"partial,omitempty"`
	FailedPath string   `json:"failed_path,omitempty"`
	Error      string   `json:"error,omitempty"`
}

var ErrConflict = errors.New("apply conflict")

func Apply(root string, cs Changeset) (Result, error) {
	return applyWithOps(root, cs, defaultApplyOps())
}

type applyOps struct {
	mkdirAll   func(string, os.FileMode) error
	remove     func(string) error
	createTemp func(string, string) (*os.File, error)
	rename     func(string, string) error
}

func defaultApplyOps() applyOps {
	return applyOps{
		mkdirAll:   os.MkdirAll,
		remove:     os.Remove,
		createTemp: os.CreateTemp,
		rename:     os.Rename,
	}
}

func applyWithOps(root string, cs Changeset, ops applyOps) (Result, error) {
	release, err := acquireApplyLock(root)
	if err != nil {
		return Result{Applied: false}, err
	}
	defer release()

	conflicts, err := verify(root, cs)
	if err != nil {
		return Result{Applied: false}, err
	}
	if len(conflicts) > 0 {
		return Result{Applied: false, Conflicts: conflicts}, ErrConflict
	}
	result := Result{Applied: false}
	for _, ch := range cs.Changes {
		full, err := resolveMutationPath(root, ch.Path)
		if err != nil {
			result.FailedPath = ch.Path
			return result, err
		}
		switch ch.Kind {
		case ChangeCreate, ChangeUpdate:
			if err := ops.mkdirAll(filepath.Dir(full), 0o755); err != nil {
				result.FailedPath = ch.Path
				return result, err
			}
			if err := writeReplacementFile(full, ch.Content, ops); err != nil {
				result.FailedPath = ch.Path
				return result, err
			}
		case ChangeDelete:
			if err := ops.remove(full); err != nil {
				result.FailedPath = ch.Path
				return result, err
			}
		default:
			result.FailedPath = ch.Path
			return result, errors.New("unknown change kind")
		}
		result.Partial = append(result.Partial, ch.Path)
	}
	return Result{Applied: true}, nil
}

func writeReplacementFile(path string, content []byte, ops applyOps) error {
	tmp, err := ops.createTemp(filepath.Dir(path), ".remork-apply-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ops.rename(tmpName, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func verify(root string, cs Changeset) ([]string, error) {
	var conflicts []string
	for _, ch := range cs.Changes {
		full, err := resolveMutationPath(root, ch.Path)
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
