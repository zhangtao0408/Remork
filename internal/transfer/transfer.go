package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteFile(localRoot, remotePath string, data []byte) error {
	full := filepath.Join(localRoot, filepath.FromSlash(remotePath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp := full + ".remork-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

func WriteLargeMeta(localRoot, remotePath string, meta any) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(localRoot, remotePath+".meta", data)
}
