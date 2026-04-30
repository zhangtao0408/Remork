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

func assertOp(t *testing.T, plan Plan, path string, kind OperationKind) {
	t.Helper()
	for _, op := range plan.Operations {
		if op.Path == path && op.Kind == kind {
			return
		}
	}
	t.Fatalf("missing op %s %s in %#v", kind, path, plan.Operations)
}
