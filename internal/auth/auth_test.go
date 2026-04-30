package auth

import (
	"net/http"
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
