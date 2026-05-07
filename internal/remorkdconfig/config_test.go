package remorkdconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExpandsHomeAndParsesConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "remorkd.toml")
	data := []byte(`
listen_addr = "0.0.0.0:17731"
allowed_roots = ["/home/me", "/scratch/me"]
large_file_threshold = "128MB"
token_file = "$HOME/.remork/remork.token"
pid_file = "$HOME/.remork/run/remorkd.pid"
log_file = "$HOME/.remork/log/remorkd.log"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:17731" {
		t.Fatalf("listen = %q", cfg.ListenAddr)
	}
	if cfg.TokenFile != filepath.Join(home, ".remork", "remork.token") {
		t.Fatalf("token file = %q", cfg.TokenFile)
	}
	if len(cfg.AllowedRoots) != 2 || cfg.AllowedRoots[1] != "/scratch/me" {
		t.Fatalf("roots = %#v", cfg.AllowedRoots)
	}
}

func TestValidateRejectsNoRoots(t *testing.T) {
	err := Validate(Config{ListenAddr: "127.0.0.1:17731"})
	if err == nil {
		t.Fatal("Validate error = nil, want root error")
	}
}

func TestDefaultPathUsesHome(t *testing.T) {
	home := "/home/me"
	if got := DefaultPath(home); got != "/home/me/.remork/remorkd.toml" {
		t.Fatalf("DefaultPath = %q", got)
	}
}
