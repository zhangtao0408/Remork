package transfer

import (
	"encoding/json"
	"fmt"
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
	tmp := full + ".remork-tmp"
	if info, err := os.Lstat(tmp); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("temporary path %q is a symlink", tmp)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, full)
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
