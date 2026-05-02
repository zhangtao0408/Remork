//go:build !darwin && !linux

package securefs

import (
	"os"

	"remork/internal/paths"
)

func OpenExistingFile(root, remotePath string) (*os.File, error) {
	full, err := paths.ResolveExistingInsideWorkspace(root, remotePath)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}
