package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			if err := writeLockMetadata(file); err != nil {
				_ = file.Close()
				_ = os.Remove(lockPath)
				return nil, err
			}
			if err := file.Close(); err != nil {
				_ = os.Remove(lockPath)
				return nil, err
			}
			return func() {
				_ = os.Remove(lockPath)
			}, nil
		}
		if errors.Is(err, os.ErrExist) {
			if stale, staleErr := isStaleApplyLock(lockPath); staleErr != nil {
				return nil, staleErr
			} else if stale {
				if removeErr := os.Remove(lockPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					return nil, removeErr
				}
				continue
			}
			return nil, fmt.Errorf("%w: %s", ErrApplyLocked, lockPath)
		}
		return nil, err
	}
}

func writeLockMetadata(file *os.File) error {
	host, _ := os.Hostname()
	_, err := fmt.Fprintf(file, "pid=%d\nhost=%s\n", os.Getpid(), host)
	return err
}

func isStaleApplyLock(lockPath string) (bool, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	pid := lockPID(string(data))
	if pid <= 0 {
		return false, nil
	}
	return !processAlive(pid), nil
}

func lockPID(data string) int {
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "pid" {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0
		}
		return pid
	}
	return 0
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
