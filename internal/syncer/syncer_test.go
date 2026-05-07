package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"remork/internal/api"
	"remork/internal/apply"
	"remork/internal/client"
	"remork/internal/daemon"
	"remork/internal/limits"
	"remork/internal/prompt"
	"remork/internal/state"
)

func TestSyncMaterializesSmallFilesAndLargeMeta(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "src", "main.txt"), []byte("hey"))
	mustWriteFile(t, filepath.Join(remote, "model.tar.gz"), []byte("12345"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 4}).Handler())
	defer srv.Close()

	stateDir := filepath.Join(local, ".remork", "state")
	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   state.NewStore(stateDir),
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
	basePath, err := state.BasePath(stateDir, "src/main.txt")
	if err != nil {
		t.Fatalf("base path: %v", err)
	}
	base, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatalf("read base cache: %v", err)
	}
	if string(base) != "hey" {
		t.Fatalf("base cache = %q, want hey", base)
	}
	if _, err := os.Stat(filepath.Join(local, "model.tar.gz.meta")); err != nil {
		t.Fatalf("missing large meta: %v", err)
	}
	largeBasePath, err := state.BasePath(stateDir, "model.tar.gz")
	if err != nil {
		t.Fatalf("large base path: %v", err)
	}
	if _, err := os.Stat(largeBasePath); !os.IsNotExist(err) {
		t.Fatalf("large file base cache exists or unexpected stat error: %v", err)
	}
}

func TestSyncHashMismatchPreservesExistingLocalFile(t *testing.T) {
	local := t.TempDir()
	remoteRoot := "/remote"
	manifestData := []byte("old\n")
	downloadData := []byte("old\n")
	revision := "rev-old"
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/manifest":
			data, err := json.Marshal(api.ManifestResponse{
				Root:     remoteRoot,
				Path:     ".",
				Revision: revision,
				Entries: []api.FileEntry{{
					Path:     "file_a.py",
					Type:     api.FileTypeFile,
					Size:     int64(len(manifestData)),
					Hash:     state.HashBytes(manifestData),
					Revision: revision,
				}},
			})
			if err != nil {
				return nil, err
			}
			return testHTTPResponse(http.StatusOK, data), nil
		case "/download":
			return testHTTPResponse(http.StatusOK, downloadData), nil
		default:
			return testHTTPResponse(http.StatusNotFound, []byte("not found")), nil
		}
	})}

	runner := NewRunner(RunnerOptions{
		Client:     client.NewWithOptions(client.Options{BaseURL: "http://remork.test", HTTP: httpClient}),
		StateStore: state.NewStore(filepath.Join(local, ".remork", "state")),
		LocalRoot:  local,
		RemoteRoot: remoteRoot,
	})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	manifestData = []byte("new\n")
	downloadData = []byte("corrupt\n")
	revision = "rev-new"
	_, err := runner.Sync(context.Background(), SyncOptions{})
	if err == nil || !strings.Contains(err.Error(), "download verification failed") {
		t.Fatalf("sync error = %v, want verification failure", err)
	}
	got, readErr := os.ReadFile(filepath.Join(local, "file_a.py"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old\n" {
		t.Fatalf("local file = %q, want old file preserved", got)
	}
}

func TestSyncRemovesStaleTransferTempBeforeDownload(t *testing.T) {
	local := t.TempDir()
	remoteRoot := "/remote"
	remoteData := []byte("remote\n")
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/manifest":
			data, err := json.Marshal(api.ManifestResponse{
				Root:     remoteRoot,
				Path:     ".",
				Revision: "rev-remote",
				Entries: []api.FileEntry{{
					Path:     "file_a.py",
					Type:     api.FileTypeFile,
					Size:     int64(len(remoteData)),
					Hash:     state.HashBytes(remoteData),
					Revision: "rev-remote",
				}},
			})
			if err != nil {
				return nil, err
			}
			return testHTTPResponse(http.StatusOK, data), nil
		case "/download":
			return testHTTPResponse(http.StatusOK, remoteData), nil
		default:
			return testHTTPResponse(http.StatusNotFound, []byte("not found")), nil
		}
	})}
	stale := filepath.Join(local, ".file_a.py.remork-old")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(RunnerOptions{
		Client:     client.NewWithOptions(client.Options{BaseURL: "http://remork.test", HTTP: httpClient}),
		StateStore: state.NewStore(filepath.Join(local, ".remork", "state")),
		LocalRoot:  local,
		RemoteRoot: remoteRoot,
	})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temp still exists or stat failed: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testHTTPResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestSyncReportsStagesAndOperationProgress(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "src", "main.txt"), []byte("hey"))
	mustWriteFile(t, filepath.Join(remote, "README.md"), []byte("readme"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	defer srv.Close()

	reporter := &recordingProgressReporter{}
	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   state.NewStore(filepath.Join(local, ".remork", "state")),
		LocalRoot:    local,
		WorkspaceRef: "lab:" + remote,
		RemoteRoot:   remote,
		Progress:     reporter,
	})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	for _, want := range []string{
		"sync: loading local state",
		"sync: scanning local changes",
		"sync: fetching remote manifest",
		"sync: applying remote changes",
	} {
		if !reporter.hasStart(want) {
			t.Fatalf("missing progress stage %q in %#v", want, reporter.events)
		}
	}
	if reporter.advances == 0 {
		t.Fatalf("expected operation progress advances, got %#v", reporter.events)
	}
}

func TestStatusReportsRemoteFileReplacedByDirectory(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("file\n"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
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
	if err := os.Remove(filepath.Join(remote, "a.txt")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(remote, "a.txt", "child.txt"), []byte("child\n"))

	status, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !containsString(status.ChangedPaths, "a.txt") && status.RemoteUpdates == 0 {
		t.Fatalf("status did not report replacement: %#v", status)
	}
}

func TestSyncFileReplacedByDirectoryDeletesLocalFileBeforeChildDownload(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("file\n"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
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
	if err := os.Remove(filepath.Join(remote, "a.txt")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(remote, "a.txt", "child.txt"), []byte("child\n"))

	result, err := runner.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("sync replacement: %v", err)
	}
	if result.Deleted != 1 || result.Downloaded != 1 {
		t.Fatalf("result = %#v, want one delete and one download", result)
	}
	got, err := os.ReadFile(filepath.Join(local, "a.txt", "child.txt"))
	if err != nil {
		t.Fatalf("read child: %v", err)
	}
	if string(got) != "child\n" {
		t.Fatalf("child = %q", got)
	}
}

func TestSyncDirectoryReplacedByFileDeletesChildrenBeforeParentDownload(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a", "child.txt"), []byte("child\n"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
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
	if err := os.Remove(filepath.Join(remote, "a", "child.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(remote, "a")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(remote, "a"), []byte("file\n"))

	result, err := runner.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("sync replacement: %v", err)
	}
	if result.Deleted != 1 || result.Downloaded != 1 {
		t.Fatalf("result = %#v, want one delete and one download", result)
	}
	got, err := os.ReadFile(filepath.Join(local, "a"))
	if err != nil {
		t.Fatalf("read replacement file: %v", err)
	}
	if string(got) != "file\n" {
		t.Fatalf("a = %q", got)
	}
	if _, err := os.Stat(filepath.Join(local, "a", "child.txt")); !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("old child should not exist: %v", err)
	}
}

func TestSyncDirectoryReplacedByFileConflictsWithUntrackedDescendant(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a", "child.txt"), []byte("child\n"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
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
	mustWriteFile(t, filepath.Join(local, "a", "extra.txt"), []byte("local-only\n"))
	if err := os.Remove(filepath.Join(remote, "a", "child.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(remote, "a")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(remote, "a"), []byte("file\n"))

	result, err := runner.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("sync replacement with conflict should not error: %v", err)
	}
	if result.Conflicts != 1 || result.Downloaded != 0 || result.Deleted != 0 {
		t.Fatalf("result = %#v, want one conflict only", result)
	}
	got, err := os.ReadFile(filepath.Join(local, "a", "extra.txt"))
	if err != nil {
		t.Fatalf("untracked descendant was removed: %v", err)
	}
	if string(got) != "local-only\n" {
		t.Fatalf("extra = %q", got)
	}
}

func TestPullQuietLargeFileReturnsPromptRequired(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "model.bin"), []byte("12345"))

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
		t.Fatalf("sync: %v", err)
	}

	_, err := runner.Pull(context.Background(), "model.bin", PullOptions{Quiet: true})
	if !errors.Is(err, prompt.ErrPromptRequired) {
		t.Fatalf("err = %v, want ErrPromptRequired", err)
	}
}

func TestPullMissingExplicitTargetReturnsError(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	defer srv.Close()

	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   state.NewStore(filepath.Join(local, ".remork", "state")),
		LocalRoot:    local,
		WorkspaceRef: "lab:" + remote,
		RemoteRoot:   remote,
	})

	_, err := runner.Pull(context.Background(), "missing.txt", PullOptions{Quiet: true})
	var missing MissingPullTargetError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want MissingPullTargetError", err)
	}
	if missing.Target != "missing.txt" {
		t.Fatalf("missing target = %q, want missing.txt", missing.Target)
	}
}

func TestPullForceLargeFileMaterializesAndClearsMeta(t *testing.T) {
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
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(local, "model.bin.meta")); err != nil {
		t.Fatalf("missing meta before pull: %v", err)
	}

	result, err := runner.Pull(context.Background(), "model.bin", PullOptions{Force: true})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if result.Downloaded != 1 {
		t.Fatalf("downloaded = %d, want 1", result.Downloaded)
	}
	got, err := os.ReadFile(filepath.Join(local, "model.bin"))
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if string(got) != "12345" {
		t.Fatalf("pulled file = %q, want 12345", got)
	}
	if _, err := os.Stat(filepath.Join(local, "model.bin.meta")); !os.IsNotExist(err) {
		t.Fatalf("meta still exists after pull: %v", err)
	}
	snap, err := store.Load(workspaceRef)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	entry := snap.Entries["model.bin"]
	if entry.Large || entry.MetaPath != "" {
		t.Fatalf("snapshot entry = %#v, want materialized non-large", entry)
	}
}

func TestPullDoesNotOverwriteMaterializedPlaceholderWithoutForce(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "model.bin"), []byte("remote"))

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
		t.Fatalf("sync: %v", err)
	}
	mustWriteFile(t, filepath.Join(local, "model.bin"), []byte("local"))

	result, err := runner.Pull(context.Background(), "model.bin", PullOptions{
		In:  strings.NewReader("Y\n"),
		Out: io.Discard,
	})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if result.Conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1; result=%#v", result.Conflicts, result)
	}
	got, err := os.ReadFile(filepath.Join(local, "model.bin"))
	if err != nil {
		t.Fatalf("read local: %v", err)
	}
	if string(got) != "local" {
		t.Fatalf("local file overwritten: %q", got)
	}

	if _, err := runner.Pull(context.Background(), "model.bin", PullOptions{Force: true}); err != nil {
		t.Fatalf("force pull: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(local, "model.bin"))
	if err != nil {
		t.Fatalf("read local after force: %v", err)
	}
	if string(got) != "remote" {
		t.Fatalf("force pull did not overwrite: %q", got)
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

func TestSyncRejectsSymlinkBaseCacheRoot(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("remote"))

	stateDir := filepath.Join(local, ".remork", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(stateDir, "base")); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 1024}).Handler())
	defer srv.Close()

	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   state.NewStore(stateDir),
		LocalRoot:    local,
		WorkspaceRef: "lab:" + remote,
		RemoteRoot:   remote,
	})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err == nil {
		t.Fatal("sync succeeded with symlink base cache root, want error")
	}
	if _, err := os.Stat(filepath.Join(outside, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside base cache file exists or unexpected stat error: %v", err)
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
	basePath, err := state.BasePath(filepath.Join(local, ".remork", "state"), "model.bin")
	if err != nil {
		t.Fatalf("base path: %v", err)
	}
	if _, err := os.Stat(basePath); err != nil {
		t.Fatalf("missing base cache after normal sync: %v", err)
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
	if _, err := os.Stat(basePath); err != nil {
		t.Fatalf("normal base cache should remain after transition to large: %v", err)
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
	basePath, err := state.BasePath(filepath.Join(local, ".remork", "state"), "model.bin")
	if err != nil {
		t.Fatalf("base path: %v", err)
	}
	base, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatalf("missing base cache after large to normal transition: %v", err)
	}
	if string(base) != "1234" {
		t.Fatalf("base cache = %q, want 1234", base)
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

func TestStatusReportsDirtyRemoteUpdatesConflictsAndLargePlaceholders(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("one\n"))
	mustWriteFile(t, filepath.Join(remote, "big.bin"), []byte("12345678"))

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

	mustWriteFile(t, filepath.Join(local, "a.txt"), []byte("local\n"))
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("remote\n"))

	status, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.LocalChanges != 1 {
		t.Fatalf("LocalChanges = %d, want 1; status=%#v", status.LocalChanges, status)
	}
	if status.Conflicts != 1 {
		t.Fatalf("Conflicts = %d, want 1; status=%#v", status.Conflicts, status)
	}
	if status.LargePlaceholders != 1 {
		t.Fatalf("LargePlaceholders = %d, want 1; status=%#v", status.LargePlaceholders, status)
	}
	if !containsString(status.ChangedPaths, "a.txt") {
		t.Fatalf("ChangedPaths = %#v, want a.txt", status.ChangedPaths)
	}
	if !containsString(status.ConflictPaths, "a.txt") {
		t.Fatalf("ConflictPaths = %#v, want a.txt", status.ConflictPaths)
	}
}

func TestStatusIgnoresLocalBindingMarkerFromRemoteManifest(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, ".remork-local.json"), []byte(`{"remote":true}`))
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("remote\n"))
	mustWriteFile(t, filepath.Join(local, ".remork-local.json"), []byte(`{"local":true}`))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 1024}).Handler())
	defer srv.Close()

	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   state.NewStore(filepath.Join(local, ".remork", "state")),
		LocalRoot:    local,
		WorkspaceRef: "lab:" + remote,
		RemoteRoot:   remote,
	})

	status, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.RemoteUpdates != 1 {
		t.Fatalf("RemoteUpdates = %d, want 1 for a.txt only; status=%#v", status.RemoteUpdates, status)
	}
	if status.Conflicts != 0 {
		t.Fatalf("Conflicts = %d, want 0; status=%#v", status.Conflicts, status)
	}
	if containsString(status.ChangedPaths, ".remork-local.json") {
		t.Fatalf("ChangedPaths includes local marker: %#v", status.ChangedPaths)
	}
	if containsString(status.ConflictPaths, ".remork-local.json") {
		t.Fatalf("ConflictPaths includes local marker: %#v", status.ConflictPaths)
	}
}

func TestSyncIgnoresLocalBindingMarkerFromRemoteManifest(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	localMarker := []byte(`{"workspace":"local"}`)
	mustWriteFile(t, filepath.Join(remote, ".remork-local.json"), []byte(`{"workspace":"remote"}`))
	mustWriteFile(t, filepath.Join(local, ".remork-local.json"), localMarker)

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 1024}).Handler())
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
	if result.Conflicts != 0 {
		t.Fatalf("Conflicts = %d, want 0; result=%#v", result.Conflicts, result)
	}
	got, err := os.ReadFile(filepath.Join(local, ".remork-local.json"))
	if err != nil {
		t.Fatalf("read local marker: %v", err)
	}
	if string(got) != string(localMarker) {
		t.Fatalf("local marker overwritten: got %q want %q", got, localMarker)
	}
}

func TestStatusRejectsUnsafeLargeMetaPath(t *testing.T) {
	parent := t.TempDir()
	remote := filepath.Join(parent, "remote")
	local := filepath.Join(parent, "local")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(parent, "outside.meta"), []byte("outside"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 1024}).Handler())
	defer srv.Close()

	workspaceRef := "lab:" + remote
	store := state.NewStore(filepath.Join(local, ".remork", "state"))
	if err := store.Save(state.Snapshot{
		WorkspaceRef: workspaceRef,
		Entries: map[string]state.TrackedFile{
			"big.bin": {Path: "big.bin", MetaPath: "../outside.meta", Type: "file", Large: true, Revision: "rev-big"},
		},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   store,
		LocalRoot:    local,
		WorkspaceRef: workspaceRef,
		RemoteRoot:   remote,
	})

	if _, err := runner.Status(context.Background()); err == nil {
		t.Fatal("Status succeeded with unsafe large meta path, want error")
	}
}

func TestBuildChangesetCreatesUpdatesDeletesAndSkipsLargeMeta(t *testing.T) {
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "updated.txt"), []byte("new\n"))
	mustWriteFile(t, filepath.Join(local, "created.txt"), []byte("created\n"))
	mustWriteFile(t, filepath.Join(local, "big.bin.meta"), []byte("{}"))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"updated.txt": {Path: "updated.txt", BaseHash: state.HashBytes([]byte("old\n")), Revision: "r1"},
			"deleted.txt": {Path: "deleted.txt", BaseHash: state.HashBytes([]byte("gone\n")), Revision: "r2"},
			"big.bin":     {Path: "big.bin", Large: true, MetaPath: "big.bin.meta", Revision: "r3"},
		},
	}

	changes, skipped, err := BuildChangesetWithOptions(local, snap, BuildChangesetOptions{
		UseIgnoreFiles:   true,
		IncludeUntracked: true,
	})
	if err != nil {
		t.Fatalf("BuildChangeset: %v", err)
	}
	if changes.ID == "" {
		t.Fatal("changeset ID is empty")
	}
	if len(changes.Changes) != 3 {
		t.Fatalf("changes = %#v skipped=%#v", changes.Changes, skipped)
	}
	if !containsSkipped(skipped, "big.bin.meta") {
		t.Fatalf("large meta edit not skipped: %#v", skipped)
	}
	want := []struct {
		path string
		kind string
		base string
		body string
	}{
		{"created.txt", "create", "", "created\n"},
		{"deleted.txt", "delete", state.HashBytes([]byte("gone\n")), ""},
		{"updated.txt", "update", state.HashBytes([]byte("old\n")), "new\n"},
	}
	for i, wantChange := range want {
		got := changes.Changes[i]
		if got.Path != wantChange.path || string(got.Kind) != wantChange.kind || got.BaseHash != wantChange.base || string(got.Content) != wantChange.body {
			t.Fatalf("change[%d] = %#v, want path=%q kind=%q base=%q body=%q", i, got, wantChange.path, wantChange.kind, wantChange.base, wantChange.body)
		}
	}
}

func TestBuildChangesetDoesNotReportCleanLargeMetaAsSkipped(t *testing.T) {
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "big.bin.meta"), []byte(`{
  "kind": "remote-large-file",
  "remote_path": "/big.bin",
  "revision": "r3",
  "pulled": false,
  "pull_command": "remork pull lab:/remote/big.bin"
}`))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"big.bin": {Path: "big.bin", Large: true, MetaPath: "big.bin.meta", Revision: "r3"},
		},
	}

	changes, skipped, err := BuildChangeset(local, snap)
	if err != nil {
		t.Fatalf("BuildChangeset: %v", err)
	}
	if len(changes.Changes) != 0 || len(skipped) != 0 {
		t.Fatalf("clean large placeholder should not be reported: changes=%#v skipped=%#v", changes.Changes, skipped)
	}
}

func TestBuildChangesetSkipsUntrackedCreatesByDefault(t *testing.T) {
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "tracked.txt"), []byte("new\n"))
	mustWriteFile(t, filepath.Join(local, "scratch.log"), []byte("local only\n"))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"tracked.txt": {Path: "tracked.txt", BaseHash: state.HashBytes([]byte("old\n")), Revision: "r1"},
		},
	}

	changes, skipped, err := BuildChangeset(local, snap)
	if err != nil {
		t.Fatalf("BuildChangeset: %v", err)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].Path != "tracked.txt" {
		t.Fatalf("changes = %#v", changes.Changes)
	}
	if !containsSkipped(skipped, "scratch.log") {
		t.Fatalf("untracked file was not skipped: %#v", skipped)
	}
}

func TestBuildChangesetSkipsUntrackedSymlinkCreateByDefault(t *testing.T) {
	local := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.txt")
	mustWriteFile(t, target, []byte("target\n"))
	if err := os.Symlink(target, filepath.Join(local, "linked.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	changes, skipped, err := BuildChangeset(local, state.Snapshot{Entries: map[string]state.TrackedFile{}})
	if err != nil {
		t.Fatalf("BuildChangeset: %v", err)
	}
	if len(changes.Changes) != 0 {
		t.Fatalf("changes = %#v", changes.Changes)
	}
	if !containsSkipped(skipped, "linked.txt") {
		t.Fatalf("untracked symlink was not skipped: %#v", skipped)
	}
}

func TestBuildChangesetIncludesExplicitUntrackedPath(t *testing.T) {
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "tracked.txt"), []byte("tracked new\n"))
	mustWriteFile(t, filepath.Join(local, "src", "new.txt"), []byte("intentional\n"))
	mustWriteFile(t, filepath.Join(local, "other.txt"), []byte("local only\n"))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"tracked.txt": {Path: "tracked.txt", BaseHash: state.HashBytes([]byte("tracked old\n")), Revision: "r1"},
		},
	}

	changes, skipped, err := BuildChangesetWithOptions(local, snap, BuildChangesetOptions{
		UseIgnoreFiles: true,
		ExplicitPaths:  []string{"src"},
	})
	if err != nil {
		t.Fatalf("BuildChangesetWithOptions: %v", err)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].Path != "src/new.txt" || string(changes.Changes[0].Content) != "intentional\n" {
		t.Fatalf("changes = %#v", changes.Changes)
	}
	if containsChange(changes.Changes, "tracked.txt") {
		t.Fatalf("non-explicit tracked modify was included: %#v", changes.Changes)
	}
	if !containsSkipped(skipped, "other.txt") {
		t.Fatalf("non-explicit untracked file was not skipped: %#v", skipped)
	}
}

func TestBuildChangesetExplicitTrackedModifyOnlyIncludesSelectedPath(t *testing.T) {
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "selected.txt"), []byte("selected new\n"))
	mustWriteFile(t, filepath.Join(local, "other.txt"), []byte("other new\n"))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"selected.txt": {Path: "selected.txt", BaseHash: state.HashBytes([]byte("selected old\n")), Revision: "r1"},
			"other.txt":    {Path: "other.txt", BaseHash: state.HashBytes([]byte("other old\n")), Revision: "r2"},
			"deleted.txt":  {Path: "deleted.txt", BaseHash: state.HashBytes([]byte("deleted old\n")), Revision: "r3"},
		},
	}

	changes, skipped, err := BuildChangesetWithOptions(local, snap, BuildChangesetOptions{
		UseIgnoreFiles: true,
		ExplicitPaths:  []string{"selected.txt"},
	})
	if err != nil {
		t.Fatalf("BuildChangesetWithOptions: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v", skipped)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].Path != "selected.txt" || string(changes.Changes[0].Kind) != "update" {
		t.Fatalf("changes = %#v", changes.Changes)
	}
}

func TestBuildChangesetExplicitDirectorySelectsChildrenNotSiblings(t *testing.T) {
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "src", "child.txt"), []byte("child new\n"))
	mustWriteFile(t, filepath.Join(local, "src-sibling.txt"), []byte("sibling new\n"))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"src/child.txt":   {Path: "src/child.txt", BaseHash: state.HashBytes([]byte("child old\n")), Revision: "r1"},
			"src-sibling.txt": {Path: "src-sibling.txt", BaseHash: state.HashBytes([]byte("sibling old\n")), Revision: "r2"},
		},
	}

	changes, skipped, err := BuildChangesetWithOptions(local, snap, BuildChangesetOptions{
		UseIgnoreFiles: true,
		ExplicitPaths:  []string{"src"},
	})
	if err != nil {
		t.Fatalf("BuildChangesetWithOptions: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v", skipped)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].Path != "src/child.txt" {
		t.Fatalf("changes = %#v", changes.Changes)
	}
}

func TestBuildChangesetIncludesUntrackedWithFlag(t *testing.T) {
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "new.txt"), []byte("intentional\n"))

	changes, skipped, err := BuildChangesetWithOptions(local, state.Snapshot{Entries: map[string]state.TrackedFile{}}, BuildChangesetOptions{
		UseIgnoreFiles:   true,
		IncludeUntracked: true,
	})
	if err != nil {
		t.Fatalf("BuildChangesetWithOptions: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v", skipped)
	}
	if len(changes.Changes) != 1 || changes.Changes[0].Path != "new.txt" {
		t.Fatalf("changes = %#v", changes.Changes)
	}
}

func TestBuildChangesetRejectsTrackedFileReplacedByDirectory(t *testing.T) {
	local := t.TempDir()
	if err := os.Mkdir(filepath.Join(local, "tracked.txt"), 0o755); err != nil {
		t.Fatalf("mkdir tracked path: %v", err)
	}
	mustWriteFile(t, filepath.Join(local, "tracked.txt", "child.txt"), []byte("child\n"))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"tracked.txt": {Path: "tracked.txt", BaseHash: state.HashBytes([]byte("before\n")), Revision: "r1"},
		},
	}

	_, _, err := BuildChangeset(local, snap)
	if err == nil || !strings.Contains(err.Error(), "replaced by a directory") {
		t.Fatalf("BuildChangeset error = %v, want tracked file replaced by directory", err)
	}
}

func TestBuildChangesetRejectsLargeApplyFileBeforeReading(t *testing.T) {
	local := t.TempDir()
	path := filepath.Join(local, "huge.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create sparse file: %v", err)
	}
	if err := file.Truncate(limits.MaxApplyFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate sparse file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close sparse file: %v", err)
	}

	_, _, err = BuildChangesetWithOptions(local, state.Snapshot{Entries: map[string]state.TrackedFile{}}, BuildChangesetOptions{
		UseIgnoreFiles:   true,
		IncludeUntracked: true,
	})
	if err == nil || !strings.Contains(err.Error(), "too large to apply") || !strings.Contains(err.Error(), "remork pull") {
		t.Fatalf("BuildChangesetWithOptions error = %v, want actionable large-file apply error", err)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSkipped(values []SkippedChange, want string) bool {
	for _, value := range values {
		if value.Path == want {
			return true
		}
	}
	return false
}

func containsChange(values []apply.Change, want string) bool {
	for _, value := range values {
		if value.Path == want {
			return true
		}
	}
	return false
}

type progressEvent struct {
	kind  string
	label string
	total int64
	delta int64
}

type recordingProgressReporter struct {
	events   []progressEvent
	advances int
}

func (r *recordingProgressReporter) Start(label string, total int64) {
	r.events = append(r.events, progressEvent{kind: "start", label: label, total: total})
}

func (r *recordingProgressReporter) Advance(delta int64) {
	r.advances++
	r.events = append(r.events, progressEvent{kind: "advance", delta: delta})
}

func (r *recordingProgressReporter) Done() {
	r.events = append(r.events, progressEvent{kind: "done"})
}

func (r *recordingProgressReporter) hasStart(label string) bool {
	for _, ev := range r.events {
		if ev.kind == "start" && ev.label == label {
			return true
		}
	}
	return false
}
