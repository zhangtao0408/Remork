package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrApplyLocked = errors.New("apply lock is already held")

func acquireApplyLock(root string) (func(), error) {
	remorkDir := filepath.Join(root, ".remork")
	if err := mkdirLockDir(remorkDir); err != nil {
		return nil, err
	}
	lockDir := filepath.Join(remorkDir, "lock")
	if err := mkdirLockDir(lockDir); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(lockDir, "apply.lock")
	file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s", ErrApplyLocked, lockPath)
		}
		return nil, err
	}
	_, writeErr := fmt.Fprintf(file, "pid=%d\n", os.Getpid())
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(lockPath)
		return nil, writeErr
	}
	if closeErr != nil {
		_ = os.Remove(lockPath)
		return nil, closeErr
	}
	return func() {
		_ = os.Remove(lockPath)
	}, nil
}

func mkdirLockDir(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0o755); err != nil {
			if errors.Is(err, os.ErrExist) {
				return mkdirLockDir(path)
			}
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("apply lock directory %q is a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("apply lock path %q is not a directory", path)
	}
	return nil
}
