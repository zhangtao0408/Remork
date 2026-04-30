package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/apply"
	"remork/internal/client"
	"remork/internal/daemon"
	"remork/internal/manifest"
	"remork/internal/planner"
	"remork/internal/state"
	"remork/internal/transfer"
)

func TestSyncPullApplyExecWorkflow(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWrite(t, filepath.Join(remote, "src", "main.txt"), []byte("hello"))
	mustWrite(t, filepath.Join(remote, "big.tar.gz"), []byte("0123456789"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 5}).Handler())
	defer srv.Close()
	c := client.New(srv.URL)

	man, err := c.Manifest(remote, ".")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	plan := planner.PlanSync(man, state.Snapshot{}, planner.Options{WorkspaceRef: "lab:" + remote})
	for _, op := range plan.Operations {
		switch op.Kind {
		case planner.OpDownload:
			data, err := c.Download(remote, op.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := transfer.WriteFile(local, op.Path, data); err != nil {
				t.Fatal(err)
			}
		case planner.OpWriteMeta:
			meta := manifest.BuildLargeMeta("lab:"+remote, op.Entry)
			if err := transfer.WriteLargeMeta(local, op.Path, meta); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(local, "big.tar.gz.meta")); err != nil {
		t.Fatalf("missing meta: %v", err)
	}

	base := state.HashBytes([]byte("hello"))
	if err := os.WriteFile(filepath.Join(local, "src", "main.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := c.Apply(remote, apply.Changeset{Changes: []apply.Change{
		{Path: "src/main.txt", Kind: apply.ChangeUpdate, BaseHash: base, Content: []byte("hello world")},
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !res.Applied {
		t.Fatalf("not applied: %#v", res)
	}
	got, _ := os.ReadFile(filepath.Join(remote, "src", "main.txt"))
	if string(got) != "hello world" {
		t.Fatalf("remote not updated: %q", got)
	}

	execRes, err := c.Exec(remote, remote, []string{"sh", "-c", "cat src/main.txt"}, 0)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if execRes.Stdout != "hello world" {
		t.Fatalf("exec stdout %q", execRes.Stdout)
	}
}

func TestApplyConflictDoesNotOverwriteRemote(t *testing.T) {
	remote := t.TempDir()
	mustWrite(t, filepath.Join(remote, "a.txt"), []byte("remote change"))
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	defer srv.Close()
	c := client.New(srv.URL)
	res, err := c.Apply(remote, apply.Changeset{Changes: []apply.Change{
		{Path: "a.txt", Kind: apply.ChangeUpdate, BaseHash: state.HashBytes([]byte("base")), Content: []byte("local")},
	}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if res.Applied {
		t.Fatal("conflict must not apply")
	}
	got, _ := os.ReadFile(filepath.Join(remote, "a.txt"))
	if string(got) != "remote change" {
		t.Fatalf("remote overwritten: %q", got)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
