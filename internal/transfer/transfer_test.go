package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestWriteFileUsesTempThenFinalName(t *testing.T) {
	root := t.TempDir()
	if err := WriteFile(root, "a.txt", []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt.remork-tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp file should not remain: %v", err)
	}
}
