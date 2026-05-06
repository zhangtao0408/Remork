package cli

import (
	"context"
	"testing"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/ops"
	"remork/internal/workspace"
)

type bindSpecProbe struct{}

func (bindSpecProbe) Status(context.Context, config.Host, string) (api.StatusResponse, error) {
	return api.StatusResponse{Roots: []string{"/data"}}, nil
}

func (bindSpecProbe) Manifest(context.Context, config.Host, config.Config, string) (api.ManifestResponse, error) {
	return api.ManifestResponse{Root: "/data/project"}, nil
}

func (bindSpecProbe) Operations(context.Context, config.Host, config.Config, string, int) ([]ops.Entry, error) {
	return nil, nil
}

func TestWorkspaceBindSpecWritesBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	if err := ExecuteHostConfigSpec(Options{HomeDir: home}, HostConfigSpec{Name: "lab", URL: "http://127.0.0.1:17731"}); err != nil {
		t.Fatalf("host spec: %v", err)
	}
	spec := WorkspaceBindSpec{HostName: "lab", RemoteRoot: "/data/project", LocalRoot: local}
	if _, err := PlanWorkspaceBind(Options{HomeDir: home, WorkingDir: local, DaemonProbe: bindSpecProbe{}}, spec); err != nil {
		t.Fatalf("PlanWorkspaceBind: %v", err)
	}
	if err := ExecuteWorkspaceBindSpec(Options{HomeDir: home, WorkingDir: local, DaemonProbe: bindSpecProbe{}}, spec); err != nil {
		t.Fatalf("ExecuteWorkspaceBindSpec: %v", err)
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if binding.Host != "lab" || binding.RemoteRoot != "/data/project" {
		t.Fatalf("binding = %#v", binding)
	}
}
