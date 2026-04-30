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

func TestSyncNormalToLargeRemovesMaterializedFile(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "model.bin"), []byte("1234"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 4}).Handler())
	defer srv.Close()

	store := state.NewStore(filepath.Join(local, ".remork", "state"))
	workspaceRef := "lab:" + remote
	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   store,
		LocalRoot:    local,
		WorkspaceRef: workspaceRef,
		RemoteRoot:   remote,
	})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "model.bin")); err != nil {
		t.Fatalf("missing materialized file after initial sync: %v", err)
	}

	mustWriteFile(t, filepath.Join(remote, "model.bin"), []byte("12345"))
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if _, err := os.Stat(filepath.Join(local, "model.bin")); !os.IsNotExist(err) {
		t.Fatalf("materialized file exists after normal to large transition: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "model.bin.meta")); err != nil {
		t.Fatalf("missing meta after normal to large transition: %v", err)
	}
	snap, err := store.Load(workspaceRef)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	entry := snap.Entries["model.bin"]
	if !entry.Large {
		t.Fatalf("snapshot Large = false, want true; entry=%#v", entry)
	}
}

func TestSyncLargeToNormalRemovesMetaPlaceholder(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "model.bin"), []byte("12345"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 4}).Handler())
	defer srv.Close()

	store := state.NewStore(filepath.Join(local, ".remork", "state"))
	workspaceRef := "lab:" + remote
	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   store,
		LocalRoot:    local,
		WorkspaceRef: workspaceRef,
		RemoteRoot:   remote,
	})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "model.bin.meta")); err != nil {
		t.Fatalf("missing meta after initial sync: %v", err)
	}

	mustWriteFile(t, filepath.Join(remote, "model.bin"), []byte("1234"))
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if _, err := os.Stat(filepath.Join(local, "model.bin")); err != nil {
		t.Fatalf("missing materialized file after large to normal transition: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "model.bin.meta")); !os.IsNotExist(err) {
		t.Fatalf("meta exists after large to normal transition: %v", err)
	}
	snap, err := store.Load(workspaceRef)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	entry := snap.Entries["model.bin"]
	if entry.Large {
		t.Fatalf("snapshot Large = true, want false; entry=%#v", entry)
	}
}

func TestSyncTargetDoesNotDeleteOutsideTarget(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "src", "a.txt"), []byte("a"))
	mustWriteFile(t, filepath.Join(remote, "README.md"), []byte("readme"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 1024}).Handler())
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

	result, err := runner.Sync(context.Background(), SyncOptions{TargetPath: "src"})
	if err != nil {
		t.Fatalf("target sync: %v", err)
	}
	if result.Deleted != 0 {
		t.Fatalf("deleted = %d, want 0; result=%#v", result.Deleted, result)
	}
	got, err := os.ReadFile(filepath.Join(local, "README.md"))
	if err != nil {
		t.Fatalf("README should remain: %v", err)
	}
	if string(got) != "readme" {
		t.Fatalf("README = %q, want readme", got)
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
