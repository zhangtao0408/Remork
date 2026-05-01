package transfer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"remork/internal/api"
)

func TestWriteDownloadedFileCreatesParentsAndContent(t *testing.T) {
	root := t.TempDir()
	err := WriteFile(root, "src/main.go", []byte("package main"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "src", "main.go"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "package main" {
		t.Fatalf("data %q", data)
	}
}

func TestWriteLargeMetaUsesOriginalNamePlusMeta(t *testing.T) {
	root := t.TempDir()
	meta := api.LargeFileMeta{Kind: "remote-large-file", RemotePath: "/big.tar.gz", Size: 200, PullCommand: "remork pull lab:/w/big.tar.gz"}
	if err := WriteLargeMeta(root, "big.tar.gz", meta); err != nil {
		t.Fatalf("meta: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "big.tar.gz.meta"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded api.LargeFileMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json: %v", err)
	}
	if decoded.PullCommand == "" {
		t.Fatal("missing pull command")
	}
}

func TestWriteFileRejectsPathEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	err := WriteFile(root, "../escape.txt", []byte("escape"))
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if _, statErr := os.Stat(filepath.Join(parent, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("outside file should not exist: %v", statErr)
	}
}

func TestWriteLargeMetaRejectsPathEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	err := WriteLargeMeta(root, "../escape.txt", api.LargeFileMeta{Kind: "remote-large-file"})
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if _, statErr := os.Stat(filepath.Join(parent, "escape.txt.meta")); !os.IsNotExist(statErr) {
		t.Fatalf("outside meta file should not exist: %v", statErr)
	}
}

func TestWriteFileRejectsSymlinkDirectoryEscape(t *testing.T) {
	local := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(local, "link")); err != nil {
		t.Fatal(err)
	}

	err := WriteFile(local, "link/escape.txt", []byte("x"))
	if err == nil {
		t.Fatal("expected symlink escape error")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("outside file should not exist: %v", statErr)
	}
}

func TestWriteLargeMetaRejectsSymlinkDirectoryEscape(t *testing.T) {
	local := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(local, "link")); err != nil {
		t.Fatal(err)
	}

	err := WriteLargeMeta(local, "link/big.bin", api.LargeFileMeta{Kind: "remote-large-file"})
	if err == nil {
		t.Fatal("expected symlink escape error")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "big.bin.meta")); !os.IsNotExist(statErr) {
		t.Fatalf("outside meta file should not exist: %v", statErr)
	}
}

func TestLocalPathAllowsNewNestedDirectories(t *testing.T) {
	root := t.TempDir()

	err := WriteFile(root, "src/new.txt", []byte("x"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "src", "new.txt")); statErr != nil {
		t.Fatalf("new nested file should exist: %v", statErr)
	}
}

func TestWriteFileUsesTempThenFinalName(t *testing.T) {
	root := t.TempDir()
	if err := WriteFile(root, "a.txt", []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt.remork-tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp file should not remain: %v", err)
	}
}

func TestWriteFileDoesNotClobberExistingTempLikeFile(t *testing.T) {
	root := t.TempDir()
	tempLike := filepath.Join(root, "a.txt.remork-tmp")
	if err := os.WriteFile(tempLike, []byte("user scratch"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(root, "a.txt", []byte("remote")); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "remote" {
		t.Fatalf("a.txt = %q, want remote", got)
	}
	scratch, err := os.ReadFile(tempLike)
	if err != nil {
		t.Fatalf("temp-like user file was removed: %v", err)
	}
	if string(scratch) != "user scratch" {
		t.Fatalf("temp-like user file = %q, want user scratch", scratch)
	}
}

func TestWriteFileFailureDoesNotCreateFinalFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blocked"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteFile(root, "blocked/a.txt", []byte("hello"))
	if err == nil {
		t.Fatal("expected write failure")
	}
	if _, statErr := os.Stat(filepath.Join(root, "blocked", "a.txt")); !os.IsNotExist(statErr) && !errors.Is(statErr, syscall.ENOTDIR) {
		t.Fatalf("final file should not exist: %v", statErr)
	}
}
