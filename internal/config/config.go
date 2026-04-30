package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Host struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	TokenEnv string `json:"token_env,omitempty"`
	NoProxy  bool   `json:"no_proxy,omitempty"`
}

type Workspace struct {
	Host       string `json:"host"`
	RemoteRoot string `json:"remote_root"`
	LocalRoot  string `json:"local_root"`
}

type Config struct {
	ClientID   string               `json:"client_id,omitempty"`
	Hosts      map[string]Host      `json:"hosts"`
	Workspaces map[string]Workspace `json:"workspaces"`
}

type Store struct {
	dir string
}

func NewStore(dir string) Store {
	return Store{dir: dir}
}

func (s Store) Save(cfg Config) error {
	if cfg.Hosts == nil {
		cfg.Hosts = map[string]Host{}
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]Workspace{}
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, "config.json"), data, 0o644)
}

func (s Store) Load() (Config, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Hosts == nil {
		cfg.Hosts = map[string]Host{}
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]Workspace{}
	}
	return cfg, nil
}

func (s Store) LoadOrDefault() (Config, error) {
	cfg, err := s.Load()
	if err == nil {
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return Config{}, err
	}
	return Config{
		Hosts:      map[string]Host{},
		Workspaces: map[string]Workspace{},
	}, nil
}

func ParseWorkspaceRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("workspace ref must be host:/absolute/path")
	}
	if !strings.HasPrefix(parts[1], "/") {
		return "", "", errors.New("workspace path must be absolute")
	}
	return parts[0], parts[1], nil
}
