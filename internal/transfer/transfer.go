package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func WriteFile(localRoot, remotePath string, data []byte) error {
	full, err := LocalPath(localRoot, remotePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(full, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

func WriteFileWith(localRoot, remotePath string, write func(io.Writer) error) error {
	return WriteFileWithOptions(localRoot, remotePath, WriteOptions{}, write)
}

type WriteOptions struct {
	Verify func(path string) error
}

func WriteFileWithOptions(localRoot, remotePath string, opts WriteOptions, write func(io.Writer) error) error {
	full, err := LocalPath(localRoot, remotePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return writeFileAtomicWithOptions(full, opts, write)
}

func writeFileAtomic(full string, write func(io.Writer) error) error {
	return writeFileAtomicWithOptions(full, WriteOptions{}, write)
}

func writeFileAtomicWithOptions(full string, opts WriteOptions, write func(io.Writer) error) error {
	parent := filepath.Dir(full)
	tmpFile, err := os.CreateTemp(parent, "."+filepath.Base(full)+".remork-*")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tmp)
		}
	}()
	if err := write(tmpFile); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(0o644); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if opts.Verify != nil {
		if err := opts.Verify(tmp); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, full); err != nil {
		return err
	}
	keepTemp = true
	return nil
}

func LocalPath(localRoot, remotePath string) (string, error) {
	root, err := filepath.Abs(localRoot)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(remotePath) || strings.HasPrefix(filepath.ToSlash(remotePath), "/") {
		return "", fmt.Errorf("remote path %q escapes local root", remotePath)
	}
	clean := path.Clean(filepath.ToSlash(remotePath))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("remote path %q escapes local root", remotePath)
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("remote path %q escapes local root", remotePath)
	}
	if err := rejectSymlinkWritePath(root, full); err != nil {
		return "", err
	}
	return full, nil
}

func CleanupStaleTemps(localRoot, remotePath string) error {
	full, err := LocalPath(localRoot, remotePath)
	if err != nil {
		return err
	}
	pattern := filepath.Join(filepath.Dir(full), "."+filepath.Base(full)+".remork-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func rejectSymlinkWritePath(root, full string) error {
	if _, err := filepath.EvalSymlinks(root); err != nil {
		return err
	}

	parentRel, err := filepath.Rel(root, filepath.Dir(full))
	if err != nil {
		return err
	}
	current := root
	if parentRel != "." {
		for _, component := range strings.Split(parentRel, string(filepath.Separator)) {
			current = filepath.Join(current, component)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("local path %q uses symlink parent and may escape local root", current)
			}
		}
	}

	info, err := os.Lstat(full)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("local path %q is a symlink", full)
	}
	return nil
}

func WriteLargeMeta(localRoot, remotePath string, meta any) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(localRoot, remotePath+".meta", data)
}
