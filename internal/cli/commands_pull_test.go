package cli

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"remork/internal/config"
	"remork/internal/daemon"
	"remork/internal/workspace"
)

func TestNormalizePullTargetAcceptsMetaPullCommandTarget(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/remote/root"}
	got, err := normalizePullTarget("lab:/remote/root/checkpoints/model.tar.gz", binding)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "checkpoints/model.tar.gz" {
		t.Fatalf("target = %q, want checkpoints/model.tar.gz", got)
	}
}

func TestNormalizePullTargetLeavesRelativeTargetUnchanged(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/remote/root"}
	got, err := normalizePullTarget("checkpoints/model.tar.gz", binding)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "checkpoints/model.tar.gz" {
		t.Fatalf("target = %q, want checkpoints/model.tar.gz", got)
	}
}

func TestNormalizePullTargetRejectsOtherWorkspaceRefs(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/remote/root"}
	_, err := normalizePullTarget("other:/remote/root/model.tar.gz", binding)
	if err == nil || !strings.Contains(err.Error(), "does not match bound host") {
		t.Fatalf("err = %v, want host mismatch", err)
	}

	_, err = normalizePullTarget("lab:/remote/root-sibling/model.tar.gz", binding)
	if err == nil || !strings.Contains(err.Error(), "outside bound remote root") {
		t.Fatalf("err = %v, want root mismatch", err)
	}
}

func TestNormalizePullTargetHandlesRootWorkspace(t *testing.T) {
	binding := workspace.Binding{Host: "lab", RemoteRoot: "/"}
	got, err := normalizePullTarget("lab:/model.tar.gz", binding)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "model.tar.gz" {
		t.Fatalf("target = %q, want model.tar.gz", got)
	}
}

func TestPullMissingTargetJSONReturnsStructuredError(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	remote := t.TempDir()
	server := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	t.Cleanup(server.Close)

	if err := config.NewStore(filepath.Join(home, ".remork")).Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: server.URL},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := workspace.WriteBinding(local, workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  remote,
		WorkspaceID: "ws-pull-missing",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-pull-missing"),
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "pull", "missing.txt", "--json")
	if err == nil {
		t.Fatal("pull missing target should fail")
	}
	if stderr.String() != "" {
		t.Fatalf("json error should not write stderr, got %q", stderr.String())
	}
	var got commandErrorJSON
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatalf("output is not strict JSON: %q: %v", stdout.String(), jsonErr)
	}
	if !strings.Contains(got.Error, "missing.txt") || !strings.Contains(got.Fix, "check the remote path") || got.Code == 0 {
		t.Fatalf("json error = %#v", got)
	}
}

func TestPullTargetNormalizationJSONReturnsStructuredError(t *testing.T) {
	for _, tt := range []struct {
		name   string
		target string
		want   string
	}{
		{name: "host mismatch", target: "other:/remote/root/model.tar.gz", want: "does not match bound host"},
		{name: "outside root", target: "lab:/remote/root-sibling/model.tar.gz", want: "outside bound remote root"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home, local, hits := boundWorkspaceWithUnexpectedDaemon(t)
			stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "pull", tt.target, "--json")
			if err == nil {
				t.Fatal("pull should fail")
			}
			if stderr.String() != "" {
				t.Fatalf("json error should not write stderr, got %q", stderr.String())
			}
			var got commandErrorJSON
			if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
				t.Fatalf("output is not strict JSON: %q: %v", stdout.String(), jsonErr)
			}
			if !strings.Contains(got.Error, tt.want) || !strings.Contains(got.Fix, "bound workspace") || got.Code != 2 {
				t.Fatalf("json error = %#v, want %q", got, tt.want)
			}
			if calls := atomic.LoadInt32(hits); calls != 0 {
				t.Fatalf("pull made %d daemon request(s) despite invalid target", calls)
			}
		})
	}
}
