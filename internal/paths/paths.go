package paths

import (
	"errors"
	"path/filepath"
	"strings"
)

var ErrPathEscape = errors.New("path escapes workspace")

func NormalizeRemotePath(p string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(p))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", ErrPathEscape
	}
	return clean, nil
}

func ResolveInsideWorkspace(root string, remotePath string) (string, error) {
	if filepath.IsAbs(remotePath) {
		return "", ErrPathEscape
	}
	norm, err := NormalizeRemotePath(remotePath)
	if err != nil {
		return "", err
	}
	rootClean, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(rootClean, filepath.FromSlash(norm))
	if !isInside(rootClean, joined) {
		return "", ErrPathEscape
	}
	return joined, nil
}

func ResolveExistingInsideWorkspace(root string, remotePath string) (string, error) {
	full, err := ResolveInsideWorkspace(root, remotePath)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	fullReal, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if !isInside(rootReal, fullReal) {
		return "", ErrPathEscape
	}
	return fullReal, nil
}

func isInside(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
