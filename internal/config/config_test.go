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

func TestParseWorkspaceRef(t *testing.T) {
	host, root, err := ParseWorkspaceRef("lab:/data/project")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if host != "lab" || root != "/data/project" {
		t.Fatalf("got %s %s", host, root)
	}
}
