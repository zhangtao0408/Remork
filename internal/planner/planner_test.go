package planner

import (
	"testing"

	"remork/internal/api"
	"remork/internal/state"
)

func TestSyncPlansSmallDownloadAndLargeMeta(t *testing.T) {
	manifest := api.ManifestResponse{Threshold: 128, Entries: []api.FileEntry{
		{Path: "a.txt", Type: api.FileTypeFile, Size: 5, Hash: "sha256:a", Revision: "rev-a"},
		{Path: "big.tar.gz", Type: api.FileTypeFile, Size: 200, Large: true, Revision: "rev-big"},
	}}
	plan := PlanSync(manifest, state.Snapshot{}, Options{WorkspaceRef: "lab:/workspace"})
	assertOp(t, plan, "a.txt", OpDownload)
	assertOp(t, plan, "big.tar.gz", OpWriteMeta)
}

func TestSyncDoesNotOverwriteDirtyFile(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "a.txt", Type: api.FileTypeFile, Size: 6, Hash: "sha256:remote", Revision: "rev-remote"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"a.txt": {Path: "a.txt", BaseHash: "sha256:base", Revision: "rev-base"},
	}}
	plan := PlanSync(manifest, snap, Options{Dirty: []state.DirtyChange{{Path: "a.txt", Kind: state.ChangeModify}}})
	assertOp(t, plan, "a.txt", OpConflict)
}

func TestPullIncludeLargeDownloadsLargeFile(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "big.tar.gz", Type: api.FileTypeFile, Size: 200, Large: true, Revision: "rev-big"},
	}}
	plan := PlanPull(manifest, state.Snapshot{}, Options{IncludeLarge: true, TargetPath: "big.tar.gz"})
	assertOp(t, plan, "big.tar.gz", OpDownload)
}

func TestPullLargePolicySwitchesBetweenMetaAndDownload(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "big.bin", Type: api.FileTypeFile, Large: true, Size: 200, Revision: "rev-big"},
	}}
	metaPlan := PlanPull(manifest, state.Snapshot{}, Options{TargetPath: "big.bin"})
	assertOp(t, metaPlan, "big.bin", OpWriteMeta)
	downloadPlan := PlanPull(manifest, state.Snapshot{}, Options{TargetPath: "big.bin", IncludeLarge: true})
	assertOp(t, downloadPlan, "big.bin", OpDownload)
}

func TestPullDirectoryPlansFilesInsideTarget(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "src", Type: api.FileTypeDir, Revision: "rev-dir"},
		{Path: "src/a.txt", Type: api.FileTypeFile, Size: 1, Hash: "sha256:a", Revision: "rev-a"},
		{Path: "other.txt", Type: api.FileTypeFile, Size: 1, Hash: "sha256:o", Revision: "rev-o"},
	}}

	plan := PlanPull(manifest, state.Snapshot{}, Options{TargetPath: "src"})

	assertOp(t, plan, "src/a.txt", OpDownload)
	assertNoOp(t, plan, "other.txt")
}

func TestSyncSameRevisionLargeToNormalDownloadsFile(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "model.bin", Type: api.FileTypeFile, Large: false, Size: 200, Revision: "same"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"model.bin": {Path: "model.bin", MetaPath: "model.bin.meta", Type: api.FileTypeFile, Revision: "same", Large: true},
	}}

	plan := PlanSync(manifest, snap, Options{})

	assertOp(t, plan, "model.bin", OpDownload)
}

func TestSyncSameRevisionNormalToLargeWritesMeta(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "model.bin", Type: api.FileTypeFile, Large: true, Size: 200, Revision: "same"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"model.bin": {Path: "model.bin", Type: api.FileTypeFile, Revision: "same", Large: false},
	}}

	plan := PlanSync(manifest, snap, Options{})

	assertOp(t, plan, "model.bin", OpWriteMeta)
}

func TestSyncSameRevisionDifferentHashDownloadsFile(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "a.txt", Type: api.FileTypeFile, Size: 4, Hash: "sha256:remote", Revision: "same"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"a.txt": {Path: "a.txt", Type: api.FileTypeFile, BaseHash: "sha256:base", Revision: "same", Large: false},
	}}

	plan := PlanSync(manifest, snap, Options{})

	assertOp(t, plan, "a.txt", OpDownload)
}

func TestSyncDirtyRemoteDeleteBecomesConflict(t *testing.T) {
	plan := PlanRemoteDeletes(state.Snapshot{Entries: map[string]state.TrackedFile{
		"deleted.txt": {Path: "deleted.txt", Revision: "rev-old"},
	}}, Options{Dirty: []state.DirtyChange{{Path: "deleted.txt", Kind: state.ChangeModify}}})
	assertOp(t, plan, "deleted.txt", OpConflict)
}

func TestSyncCleanRemoteDeletePlansDelete(t *testing.T) {
	plan := PlanRemoteDeletes(state.Snapshot{Entries: map[string]state.TrackedFile{
		"deleted.txt": {Path: "deleted.txt", Revision: "rev-old"},
	}}, Options{})
	assertOp(t, plan, "deleted.txt", OpDelete)
}

func TestSyncFileReplacedByDirectoryDeletesStaleFileBeforeChildren(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "a.txt", Type: api.FileTypeDir, Revision: "rev-dir"},
		{Path: "a.txt/child.txt", Type: api.FileTypeFile, Hash: "sha256:child", Revision: "rev-child"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"a.txt": {Path: "a.txt", Type: api.FileTypeFile, BaseHash: "sha256:old", Revision: "rev-old"},
	}}

	plan := PlanSync(manifest, snap, Options{})

	assertOp(t, plan, "a.txt", OpDelete)
	assertOp(t, plan, "a.txt/child.txt", OpDownload)
}

func TestSyncDirtyFileReplacedByDirectoryBlocksChildren(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "a.txt", Type: api.FileTypeDir, Revision: "rev-dir"},
		{Path: "a.txt/child.txt", Type: api.FileTypeFile, Hash: "sha256:child", Revision: "rev-child"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"a.txt": {Path: "a.txt", Type: api.FileTypeFile, BaseHash: "sha256:old", Revision: "rev-old"},
	}}

	plan := PlanSync(manifest, snap, Options{Dirty: []state.DirtyChange{{Path: "a.txt", Kind: state.ChangeModify}}})

	assertOp(t, plan, "a.txt", OpConflict)
	assertNoOp(t, plan, "a.txt/child.txt")
}

func TestSyncDirectoryReplacedByFileDeletesChildrenBeforeParentDownload(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "a", Type: api.FileTypeFile, Hash: "sha256:new", Revision: "rev-new"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"a/child.txt": {Path: "a/child.txt", Type: api.FileTypeFile, BaseHash: "sha256:old", Revision: "rev-old"},
	}}

	plan := PlanSync(manifest, snap, Options{})

	assertOp(t, plan, "a/child.txt", OpDelete)
	assertOp(t, plan, "a", OpDownload)
	assertBefore(t, plan, "a/child.txt", OpDelete, "a", OpDownload)
}

func TestSyncDirectoryReplacedByFileConflictsOnDirtyDescendant(t *testing.T) {
	manifest := api.ManifestResponse{Entries: []api.FileEntry{
		{Path: "a", Type: api.FileTypeFile, Hash: "sha256:new", Revision: "rev-new"},
	}}
	snap := state.Snapshot{Entries: map[string]state.TrackedFile{
		"a/child.txt": {Path: "a/child.txt", Type: api.FileTypeFile, BaseHash: "sha256:old", Revision: "rev-old"},
	}}

	plan := PlanSync(manifest, snap, Options{Dirty: []state.DirtyChange{{Path: "a/extra.txt", Kind: state.ChangeCreate}}})

	assertOp(t, plan, "a", OpConflict)
	assertNoOpKind(t, plan, "a", OpDownload)
	assertNoOp(t, plan, "a/child.txt")
}

func assertOp(t *testing.T, plan Plan, path string, kind OperationKind) {
	t.Helper()
	for _, op := range plan.Operations {
		if op.Path == path && op.Kind == kind {
			return
		}
	}
	t.Fatalf("missing op %s %s in %#v", kind, path, plan.Operations)
}

func assertBefore(t *testing.T, plan Plan, firstPath string, firstKind OperationKind, secondPath string, secondKind OperationKind) {
	t.Helper()
	first := -1
	second := -1
	for i, op := range plan.Operations {
		if op.Path == firstPath && op.Kind == firstKind {
			first = i
		}
		if op.Path == secondPath && op.Kind == secondKind {
			second = i
		}
	}
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("want %s %s before %s %s in %#v", firstKind, firstPath, secondKind, secondPath, plan.Operations)
	}
}

func assertNoOp(t *testing.T, plan Plan, path string) {
	t.Helper()
	for _, op := range plan.Operations {
		if op.Path == path {
			t.Fatalf("unexpected op for %s in %#v", path, plan.Operations)
		}
	}
}

func assertNoOpKind(t *testing.T, plan Plan, path string, kind OperationKind) {
	t.Helper()
	for _, op := range plan.Operations {
		if op.Path == path && op.Kind == kind {
			t.Fatalf("unexpected op %s for %s in %#v", kind, path, plan.Operations)
		}
	}
}
