package apply

import (
	"os"
	"path/filepath"
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

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
