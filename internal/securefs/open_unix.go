//go:build darwin || linux

package securefs

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"remork/internal/paths"
)

// OpenExistingFile opens a workspace-relative path without following symlinks
// in any path component. The descriptor walk keeps validation and use tied to
// opened directory descriptors instead of re-checking path strings.
func OpenExistingFile(root, remotePath string) (*os.File, error) {
	norm, err := paths.NormalizeRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, err
	}

	current, err := unix.Open(rootReal, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, mapSymlinkErr(err)
	}
	defer func() {
		if current >= 0 {
			_ = unix.Close(current)
		}
	}()

	parts := strings.Split(norm, "/")
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		last := i == len(parts)-1
		if last {
			fd, err := unix.Openat(current, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				return nil, mapSymlinkErr(err)
			}
			return os.NewFile(uintptr(fd), filepath.Join(rootReal, filepath.FromSlash(norm))), nil
		}

		next, err := unix.Openat(current, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, mapSymlinkErr(err)
		}
		_ = unix.Close(current)
		current = next
	}
	return nil, paths.ErrPathEscape
}

func mapSymlinkErr(err error) error {
	if err == unix.ELOOP || err == unix.ENOTDIR {
		return paths.ErrPathEscape
	}
	return err
}
