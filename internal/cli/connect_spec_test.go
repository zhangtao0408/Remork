package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/ops"
	"remork/internal/workspace"
)

type connectProbe struct {
	roots        []string
	manifestRoot string
}

func (p *connectProbe) Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error) {
	return api.StatusResponse{Roots: p.roots}, nil
}

func (p *connectProbe) Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error) {
	p.manifestRoot = root
	return api.ManifestResponse{Root: root, Path: "."}, nil
}

func (p *connectProbe) Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error) {
	return nil, nil
}

func TestDeriveConnectHostName(t *testing.T) {
	got, err := deriveConnectHostName("http://lab.example.internal:17731")
	if err != nil {
		t.Fatalf("deriveConnectHostName: %v", err)
	}
	if got != "lab-example-internal-17731" {
		t.Fatalf("host = %q, want lab-example-internal-17731", got)
	}
}

func TestExecuteConnectSpecWritesHostTokenAndBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	probe := &connectProbe{roots: []string{"/home/me"}}
	err := ExecuteConnectSpec(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: probe,
	}, ConnectSpec{
		URL:           "http://lab.example.internal:17731",
		HostName:      "lab",
		Token:         "secret-token",
		SelectedRoot:  "/home/me",
		WorkspacePath: "project-a",
		FirstSync:     false,
	})
	if err != nil {
		t.Fatalf("ExecuteConnectSpec: %v", err)
	}

	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatal(err)
	}
	host := cfg.Hosts["lab"]
	if host.URL != "http://lab.example.internal:17731" {
		t.Fatalf("host URL = %q", host.URL)
	}
	if host.TokenFile == "" {
		t.Fatal("host token file was not saved")
	}
	data, err := os.ReadFile(host.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret-token\n" {
		t.Fatalf("token file = %q, want secret-token newline", data)
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if binding.Host != "lab" || binding.RemoteRoot != "/home/me/project-a" {
		t.Fatalf("binding = %#v", binding)
	}
	if probe.manifestRoot != "/home/me/project-a" {
		t.Fatalf("manifest root = %q, want /home/me/project-a", probe.manifestRoot)
	}
}

func TestExecuteConnectSpecRejectsManifestOutsideRootsWithoutBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	probe := &connectProbe{roots: []string{"/home/me"}}
	err := ExecuteConnectSpec(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: probe,
	}, ConnectSpec{
		URL:           "http://lab.example.internal:17731",
		HostName:      "lab",
		SelectedRoot:  "/home/me",
		WorkspacePath: "/var/tmp/project",
	})
	if err == nil {
		t.Fatal("ExecuteConnectSpec error = nil, want outside-root error")
	}
	if _, _, resolveErr := workspace.ResolveFrom(local); resolveErr == nil {
		t.Fatal("workspace binding should not be written on failed connect")
	}
}
