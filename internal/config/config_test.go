package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRoundTripHostAndWorkspace(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cfg := Config{Hosts: map[string]Host{"lab": {Name: "lab", URL: "http://remork-daemon.example.internal:7731"}}, Workspaces: map[string]Workspace{
		"lab:/data/project": {Host: "lab", RemoteRoot: "/data/project", LocalRoot: "/tmp/project"},
	}}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Hosts["lab"].URL == "" || got.Workspaces["lab:/data/project"].LocalRoot == "" {
		t.Fatalf("bad config: %#v", got)
	}
}

func TestHostConfigStoresTokenReferenceAndNoProxy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cfg := Config{
		ClientID: "tao-macbook",
		Hosts: map[string]Host{"lab-a": {
			Name:     "lab-a",
			URL:      "http://remork-daemon.example.internal:17731",
			TokenEnv: "REMORK_LAB_A_TOKEN",
			NoProxy:  true,
		}},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ClientID != "tao-macbook" {
		t.Fatalf("client id %q", got.ClientID)
	}
	host := got.Hosts["lab-a"]
	if host.Name != "lab-a" || host.URL != "http://remork-daemon.example.internal:17731" || host.TokenEnv != "REMORK_LAB_A_TOKEN" || !host.NoProxy {
		t.Fatalf("bad host: %#v", host)
	}
}

func TestHostConfigStoresTokenFile(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := Config{
		Hosts: map[string]Host{
			"lab": {
				Name:      "lab",
				URL:       "http://127.0.0.1:17731",
				TokenFile: "/Users/me/.remork/tokens/lab.token",
				NoProxy:   true,
			},
		},
		Workspaces: map[string]Workspace{},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	host := loaded.Hosts["lab"]
	if host.TokenFile != "/Users/me/.remork/tokens/lab.token" {
		t.Fatalf("token_file = %q, want saved path", host.TokenFile)
	}
	if !host.NoProxy {
		t.Fatal("no_proxy should be preserved")
	}
}

func TestDefaultConfigWhenMissing(t *testing.T) {
	got, err := NewStore(t.TempDir()).LoadOrDefault()
	if err != nil {
		t.Fatalf("load default: %v", err)
	}
	if got.Hosts == nil {
		t.Fatal("Hosts map should be initialized")
	}
	if got.Workspaces == nil {
		t.Fatal("Workspaces map should be initialized")
	}
}

func TestStorePreservesUnknownTopLevelFields(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	raw := []byte(`{
  "client_id": "tao-macbook",
  "future": {"enabled": true},
  "hosts": {},
  "workspaces": {}
}`)
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write seed config: %v", err)
	}

	store := NewStore(dir)
	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.Hosts["lab"] = Host{Name: "lab", URL: "http://remork-daemon.example.internal:7731"}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(saved, &decoded); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if _, ok := decoded["future"]; !ok {
		t.Fatalf("future top-level field was not preserved: %s", saved)
	}
}

func TestWriteFileAtomicWritesTargetAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := writeFileAtomic(target, []byte(`{"ok":true}`), 0o640); err != nil {
		t.Fatalf("write atomic: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("target content %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("unexpected temp files left behind: %#v", entries)
	}
}

func TestParseWorkspaceRef(t *testing.T) {
	host, root, err := ParseWorkspaceRef("lab:/data/project")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if host != "lab" || root != "/data/project" {
		t.Fatalf("got %s %s", host, root)
	}
}
