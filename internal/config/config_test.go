package config

import "testing"

func TestConfigRoundTripHostAndWorkspace(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cfg := Config{Hosts: map[string]Host{"lab": {Name: "lab", URL: "http://10.0.0.12:7731"}}, Workspaces: map[string]Workspace{
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
			URL:      "http://10.0.0.12:17731",
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
	if host.Name != "lab-a" || host.URL != "http://10.0.0.12:17731" || host.TokenEnv != "REMORK_LAB_A_TOKEN" || !host.NoProxy {
		t.Fatalf("bad host: %#v", host)
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

func TestParseWorkspaceRef(t *testing.T) {
	host, root, err := ParseWorkspaceRef("lab:/data/project")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if host != "lab" || root != "/data/project" {
		t.Fatalf("got %s %s", host, root)
	}
}
