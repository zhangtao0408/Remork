package apply

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"remork/internal/state"
)

func TestApplyUpdateSucceedsWhenBaseMatches(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before"))
	change := Change{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("before")), Content: []byte("after")}
	result, err := Apply(root, Changeset{Changes: []Change{change}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !result.Applied {
		t.Fatalf("not applied: %#v", result)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "after" {
		t.Fatalf("data %q", data)
	}
}

func TestApplyRejectsWhenRemoteChanged(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("remote"))
	change := Change{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("base")), Content: []byte("after")}
	result, err := Apply(root, Changeset{Changes: []Change{change}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Applied {
		t.Fatal("must not apply conflict")
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "remote" {
		t.Fatalf("remote overwritten: %q", data)
	}
}

func TestApplyCreateAndDelete(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "delete.txt"), []byte("gone"))
	cs := Changeset{Changes: []Change{
		{Path: "new.txt", Kind: ChangeCreate, Content: []byte("new")},
		{Path: "delete.txt", Kind: ChangeDelete, BaseHash: state.HashBytes([]byte("gone"))},
	}}
	if _, err := Apply(root, cs); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete still exists: %v", err)
	}
}

func TestApplyCreateConflictsWhenRemoteExists(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "exists.txt"), []byte("remote"))
	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "exists.txt", Kind: ChangeCreate, Content: []byte("local")},
	}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Applied || len(result.Conflicts) != 1 {
		t.Fatalf("bad result: %#v", result)
	}
}

func TestApplyDeleteConflictsWhenRemoteChanged(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("remote change"))
	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "a.txt", Kind: ChangeDelete, BaseHash: state.HashBytes([]byte("base"))},
	}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Applied {
		t.Fatal("delete conflict must not apply")
	}
}

func TestApplyRejectsWholeChangesetWhenOneChangeConflicts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ok.txt"), []byte("ok-base"))
	mustWrite(t, filepath.Join(root, "conflict.txt"), []byte("remote-change"))
	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "ok.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("ok-base")), Content: []byte("ok-after")},
		{Path: "conflict.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("old-base")), Content: []byte("local-after")},
	}})
	if err == nil {
		t.Fatal("expected conflict")
	}
	if result.Applied {
		t.Fatal("changeset must not partially apply")
	}
	data, _ := os.ReadFile(filepath.Join(root, "ok.txt"))
	if string(data) != "ok-base" {
		t.Fatalf("valid change was partially applied: %q", data)
	}
}

func TestApplyRetrySameChangesetIsConflictWithoutChangingAppliedContent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before"))
	change := Change{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("before")), Content: []byte("after")}
	if _, err := Apply(root, Changeset{Changes: []Change{change}}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	result, err := Apply(root, Changeset{Changes: []Change{change}})
	if err == nil {
		t.Fatal("expected retry conflict")
	}
	if result.Applied {
		t.Fatal("retry must not apply again")
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "after" {
		t.Fatalf("content changed on retry: %q", data)
	}
}

func TestApplyBinaryUpdate(t *testing.T) {
	root := t.TempDir()
	before := []byte{0, 1, 2, 3}
	after := []byte{0, 9, 8, 7}
	mustWrite(t, filepath.Join(root, "blob.bin"), before)
	_, err := Apply(root, Changeset{Changes: []Change{
		{Path: "blob.bin", Kind: ChangeUpdate, BaseHash: state.HashBytes(before), Content: after},
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "blob.bin"))
	if string(data) != string(after) {
		t.Fatalf("binary content %#v", data)
	}
}

func TestApplyReportsPartialFailure(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("a-before"))
	mustWrite(t, filepath.Join(root, "b.txt"), []byte("b-before"))
	renameErr := errors.New("injected rename failure")
	ops := defaultApplyOps()
	ops.rename = func(oldpath, newpath string) error {
		if filepath.Base(newpath) == "b.txt" {
			return renameErr
		}
		return os.Rename(oldpath, newpath)
	}

	result, err := applyWithOps(root, Changeset{Changes: []Change{
		{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("a-before")), Content: []byte("a-after")},
		{Path: "b.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("b-before")), Content: []byte("b-after")},
	}}, ops)
	if !errors.Is(err, renameErr) {
		t.Fatalf("error = %v, want injected rename failure", err)
	}
	if result.Applied {
		t.Fatalf("partial failure reported applied: %#v", result)
	}
	if !reflect.DeepEqual(result.Partial, []string{"a.txt"}) {
		t.Fatalf("partial = %#v, want [a.txt]", result.Partial)
	}
	if result.FailedPath != "b.txt" {
		t.Fatalf("failed path = %q, want b.txt", result.FailedPath)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a-after" {
		t.Fatalf("a.txt = %q, want a-after", data)
	}
	data, err = os.ReadFile(filepath.Join(root, "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "b-before" {
		t.Fatalf("b.txt = %q, want b-before", data)
	}
}

func TestApplyFailsWhenApplyLockExists(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before"))
	lockDir := filepath.Join(root, ".remork", "lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockFile := filepath.Join(lockDir, "apply.lock")
	if err := os.WriteFile(lockFile, []byte("held"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("before")), Content: []byte("after")},
	}})
	if err == nil {
		t.Fatal("expected apply lock error")
	}
	if result.Applied {
		t.Fatalf("locked apply reported applied: %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before" {
		t.Fatalf("locked apply mutated file: %q", data)
	}
}

func TestApplyReclaimsStalePidLockFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before"))
	lockDir := filepath.Join(root, ".remork", "lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockFile := filepath.Join(lockDir, "apply.lock")
	if err := os.WriteFile(lockFile, []byte("pid=999999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(root, Changeset{Changes: []Change{
		{Path: "a.txt", Kind: ChangeUpdate, BaseHash: state.HashBytes([]byte("before")), Content: []byte("after")},
	}})
	if err != nil {
		t.Fatalf("apply should reclaim stale lock: %v", err)
	}
	if !result.Applied {
		t.Fatalf("result = %#v, want applied", result)
	}
	data, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after" {
		t.Fatalf("file = %q, want after", data)
	}
}

func TestApplyUpdateDoesNotFollowPredictableTempSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, []byte("outside-base"))
	mustWrite(t, filepath.Join(root, "safe.txt"), []byte("safe-base"))
	if err := os.Symlink(outside, filepath.Join(root, "safe.txt.remork-apply")); err != nil {
		t.Fatalf("temp symlink: %v", err)
	}

	result, err := Apply(root, Changeset{Changes: []Change{
		{
			Path:     "safe.txt",
			Kind:     ChangeUpdate,
			BaseHash: state.HashBytes([]byte("safe-base")),
			Content:  []byte("safe-after"),
		},
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !result.Applied {
		t.Fatalf("not applied: %#v", result)
	}
	outsideData, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideData) != "outside-base" {
		t.Fatalf("outside file was modified: %q", outsideData)
	}
	data, err := os.ReadFile(filepath.Join(root, "safe.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "safe-after" {
		t.Fatalf("safe file content: %q", data)
	}
}

func TestApplyRejectsCreateThroughSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}

	_, err := Apply(root, Changeset{Changes: []Change{
		{Path: "linked/escape.txt", Kind: ChangeCreate, Content: []byte("outside")},
	}})
	if err == nil {
		t.Fatal("Apply accepted create through symlink parent")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was touched: %v", err)
	}
}

func TestApplyRejectsUpdateOfSymlinkFile(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, []byte("outside-base"))
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	_, err := Apply(root, Changeset{Changes: []Change{
		{
			Path:     "link.txt",
			Kind:     ChangeUpdate,
			BaseHash: state.HashBytes([]byte("outside-base")),
			Content:  []byte("outside-after"),
		},
	}})
	if err == nil {
		t.Fatal("Apply accepted update of symlink file")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside-base" {
		t.Fatalf("outside file was modified: %q", data)
	}
}

func TestApplyRejectsDeleteOfSymlinkFile(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, []byte("outside-base"))
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	_, err := Apply(root, Changeset{Changes: []Change{
		{
			Path:     "link.txt",
			Kind:     ChangeDelete,
			BaseHash: state.HashBytes([]byte("outside-base")),
		},
	}})
	if err == nil {
		t.Fatal("Apply accepted delete of symlink file")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was removed: %v", err)
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
