package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokenRejectsEmptyTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveToken("", path); err == nil {
		t.Fatal("resolveToken should reject empty token file")
	}
}

func TestResolveTokenRejectsTokenAndTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveToken("flag-token", path); err == nil {
		t.Fatal("resolveToken should reject --token with --token-file")
	}
}

func TestResolveTokenTrimsTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(" file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	token, err := resolveToken("", path)
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if token != "file-token" {
		t.Fatalf("token %q, want file-token", token)
	}
}
