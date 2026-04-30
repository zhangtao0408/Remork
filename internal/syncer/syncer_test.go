package syncer

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/client"
	"remork/internal/daemon"
	"remork/internal/state"
)

func TestSyncMaterializesSmallFilesAndLargeMeta(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "src", "main.txt"), []byte("hey"))
	mustWriteFile(t, filepath.Join(remote, "model.tar.gz"), []byte("12345"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 4}).Handler())
	defer srv.Close()

	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   state.NewStore(filepath.Join(local, ".remork", "state")),
		LocalRoot:    local,
		WorkspaceRef: "lab:" + remote,
		RemoteRoot:   remote,
	})
	result, err := runner.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Downloaded != 1 || result.MetaWritten != 1 {
		t.Fatalf("result = %#v, want downloaded 1 and meta 1", result)
	}
	got, err := os.ReadFile(filepath.Join(local, "src", "main.txt"))
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(got) != "hey" {
		t.Fatalf("local file = %q, want hey", got)
	}
	if _, err := os.Stat(filepath.Join(local, "model.tar.gz.meta")); err != nil {
		t.Fatalf("missing large meta: %v", err)
	}
}

func TestSyncPreservesDirtyLocalFile(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("base"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 4}).Handler())
	defer srv.Close()

	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   state.NewStore(filepath.Join(local, ".remork", "state")),
		LocalRoot:    local,
		WorkspaceRef: "lab:" + remote,
		RemoteRoot:   remote,
	})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	mustWriteFile(t, filepath.Join(local, "a.txt"), []byte("local-dirty"))
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("new"))

	result, err := runner.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if result.Conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1; result=%#v", result.Conflicts, result)
	}
	got, err := os.ReadFile(filepath.Join(local, "a.txt"))
	if err != nil {
		t.Fatalf("read local dirty file: %v", err)
	}
	if string(got) != "local-dirty" {
		t.Fatalf("local file overwritten: %q", got)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
