package remorkdconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	ListenAddr         string   `toml:"listen_addr"`
	AllowedRoots       []string `toml:"allowed_roots"`
	LargeFileThreshold string   `toml:"large_file_threshold"`
	TokenFile          string   `toml:"token_file,omitempty"`
	PIDFile            string   `toml:"pid_file"`
	LogFile            string   `toml:"log_file"`
}

func DefaultPath(home string) string {
	return filepath.Join(home, ".remork", "remorkd.toml")
}

func Default(home string) Config {
	return Config{
		ListenAddr:         "0.0.0.0:17731",
		LargeFileThreshold: "128MB",
		TokenFile:          filepath.Join(home, ".remork", "remork.token"),
		PIDFile:            filepath.Join(home, ".remork", "run", "remorkd.pid"),
		LogFile:            filepath.Join(home, ".remork", "log", "remorkd.log"),
	}
}

func Load(path string, home string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.expand(home)
	return cfg, Validate(cfg)
}

func Save(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Validate(cfg Config) error {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return fmt.Errorf("listen_addr is required")
	}
	if len(cfg.AllowedRoots) == 0 {
		return fmt.Errorf("allowed_roots must contain at least one root")
	}
	for _, root := range cfg.AllowedRoots {
		if !strings.HasPrefix(strings.TrimSpace(root), "/") {
			return fmt.Errorf("allowed root %q must be absolute", root)
		}
	}
	return nil
}

func (cfg *Config) expand(home string) {
	cfg.TokenFile = ExpandHome(cfg.TokenFile, home)
	cfg.PIDFile = ExpandHome(cfg.PIDFile, home)
	cfg.LogFile = ExpandHome(cfg.LogFile, home)
}

func ExpandHome(path string, home string) string {
	if strings.HasPrefix(path, "$HOME/") {
		return filepath.Join(home, strings.TrimPrefix(path, "$HOME/"))
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}
