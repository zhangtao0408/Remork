package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplySkipsUntrackedFilesUnlessExplicit(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("tracked.txt", "base\n")
	h.bindAndSync()
	h.writeLocal("tracked.txt", "changed\n")
	h.writeLocal("local.log", "do-not-upload\n")

	out := h.runInLocal("apply", "--yes")
	mustContain(t, out, "applied 1")
	h.assertRemote("tracked.txt", "changed\n")
	if _, err := os.Stat(filepath.Join(h.remote, "local.log")); !os.IsNotExist(err) {
		t.Fatal("untracked local.log was uploaded")
	}

	h.writeLocal("src/new.txt", "intentional\n")
	h.writeLocal("tracked.txt", "unrelated dirty edit\n")
	h.runInLocal("apply", "--yes", "src/new.txt")
	h.assertRemote("src/new.txt", "intentional\n")
	h.assertRemote("tracked.txt", "changed\n")
}

func TestApplyIncludesUntrackedWithFlag(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("tracked.txt", "base\n")
	h.bindAndSync()
	h.writeLocal("new.txt", "intentional\n")
	h.writeLocal("src/other.txt", "also intentional\n")

	out := h.runInLocal("apply", "--yes", "--include-untracked")
	mustContain(t, out, "applied 2")
	h.assertRemote("new.txt", "intentional\n")
	h.assertRemote("src/other.txt", "also intentional\n")
}

func TestApplyWarnsWhenOnlyUntrackedFilesWereSkipped(t *testing.T) {
	h := newProductHarness(t)
	h.bindAndSync()
	h.writeLocal("local.log", "do-not-upload\n")

	out := h.runInLocal("apply", "--yes")
	mustContain(t, out, "applied 0")
	mustContain(t, out, "Skipped untracked or ignored files")
	if _, err := os.Stat(filepath.Join(h.remote, "local.log")); !os.IsNotExist(err) {
		t.Fatal("untracked local.log was uploaded")
	}
}
