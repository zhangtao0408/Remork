package auth

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("REMORK_TEST_TOKEN", "abc123")

	token, err := TokenFromEnv("REMORK_TEST_TOKEN")
	if err != nil {
		t.Fatalf("TokenFromEnv: %v", err)
	}
	if token != "abc123" {
		t.Fatalf("token %q, want abc123", token)
	}
}

func TestTokenFromEnvRejectsEmptyValue(t *testing.T) {
	t.Setenv("REMORK_EMPTY_TOKEN", "")

	if _, err := TokenFromEnv("REMORK_EMPTY_TOKEN"); err == nil {
		t.Fatal("TokenFromEnv should reject empty token env")
	}
}

func TestTokenFromFileTrimsValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(" file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := TokenFromFile(path)
	if err != nil {
		t.Fatalf("TokenFromFile: %v", err)
	}
	if token != "file-token" {
		t.Fatalf("token = %q, want file-token", token)
	}
}

func TestTokenFromFileRejectsEmptyValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := TokenFromFile(path); err == nil {
		t.Fatal("TokenFromFile error = nil, want empty token error")
	}
}

func TestTokenFromSourceUsesEnvBeforeFile(t *testing.T) {
	t.Setenv("REMORK_TEST_TOKEN", "env-token")
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := TokenFromSource(TokenSource{Env: "REMORK_TEST_TOKEN", File: path})
	if err != nil {
		t.Fatalf("TokenFromSource: %v", err)
	}
	if token != "env-token" {
		t.Fatalf("token = %q, want env-token", token)
	}
}

func TestTokenFromSourceReturnsEmptyWhenNoSource(t *testing.T) {
	token, err := TokenFromSource(TokenSource{})
	if err != nil {
		t.Fatalf("TokenFromSource: %v", err)
	}
	if token != "" {
		t.Fatalf("token = %q, want empty token", token)
	}
}

func TestAuthorizeBearerToken(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://remork.test/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer abc123")

	if err := Authorize(req, "abc123"); err != nil {
		t.Fatalf("Authorize valid token: %v", err)
	}

	req.Header.Set("Authorization", "Bearer wrong")
	if err := Authorize(req, "abc123"); err == nil {
		t.Fatal("Authorize should reject wrong token")
	}
}
