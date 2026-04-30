package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const MarkerName = ".remork-local.json"

type Binding struct {
	Version     int    `json:"version"`
	Host        string `json:"host"`
	RemoteRoot  string `json:"remote_root"`
	WorkspaceID string `json:"workspace_id"`
	StateDir    string `json:"state_dir"`
	Token       string `json:"-"`
}

func WriteBinding(localRoot string, binding Binding) error {
	if binding.Token != "" {
		return errors.New("workspace binding must not contain token secrets")
	}
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(localRoot, MarkerName)
	tmp, err := os.CreateTemp(localRoot, MarkerName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func ResolveFrom(start string) (Binding, string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return Binding{}, "", err
	}
	for {
		path := filepath.Join(dir, MarkerName)
		data, err := os.ReadFile(path)
		if err == nil {
			var binding Binding
			if err := json.Unmarshal(data, &binding); err != nil {
				return Binding{}, "", err
			}
			return binding, dir, nil
		}
		if !os.IsNotExist(err) {
			return Binding{}, "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Binding{}, "", os.ErrNotExist
		}
		dir = parent
	}
}
