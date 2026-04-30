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
	return full, nil
}

func WriteLargeMeta(localRoot, remotePath string, meta any) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(localRoot, remotePath+".meta", data)
}
