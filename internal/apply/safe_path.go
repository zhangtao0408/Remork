package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"remork/internal/paths"
)

func resolveMutationPath(root, remotePath string) (string, error) {
	full, err := paths.ResolveInsideWorkspace(root, remotePath)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, full)
	if err != nil {
		return "", err
	}
	current := rootAbs
	parentRel := filepath.Dir(rel)
	if parentRel != "." {
		for _, part := range strings.Split(parentRel, string(filepath.Separator)) {
			if part == "" || part == "." {
				continue
			}
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("apply path %q uses symlink parent %q", remotePath, current)
			}
		}
	}
	if info, err := os.Lstat(full); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("apply path %q is a symlink", remotePath)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	fullRealParent := filepath.Dir(full)
	if _, err := os.Stat(fullRealParent); err == nil {
		parentReal, err := filepath.EvalSymlinks(fullRealParent)
		if err != nil {
			return "", err
		}
		if !insidePath(rootReal, parentReal) {
			return "", paths.ErrPathEscape
		}
	}
	return full, nil
}

func insidePath(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
