package cli

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/client"
	"remork/internal/config"
)

func TestIsAuthHTTPErrorDetectsUnauthorizedAndForbidden(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		if !isAuthHTTPError(&client.HTTPError{StatusCode: code}) {
			t.Fatalf("status %d should be auth error", code)
		}
	}
	if isAuthHTTPError(&client.HTTPError{StatusCode: http.StatusNotFound}) {
		t.Fatal("404 should not be auth error")
	}
}

func TestUpdateHostTokenFileWritesTrimmedToken(t *testing.T) {
	home := t.TempDir()
	host := config.Host{Name: "lab", URL: "http://127.0.0.1:17731"}
	updated, err := updateHostTokenFile(home, host, " new-token \n")
	if err != nil {
		t.Fatalf("updateHostTokenFile: %v", err)
	}
	if updated.TokenFile == "" {
		t.Fatal("TokenFile should be set")
	}
	data, err := os.ReadFile(updated.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-token\n" {
		t.Fatalf("token file = %q, want trimmed token newline", data)
	}
	if !strings.Contains(updated.TokenFile, filepath.Join(".remork", "tokens", "lab.token")) {
		t.Fatalf("token path = %q, want default lab token path", updated.TokenFile)
	}
}

func TestAuthRecoveryDoesNotHandleNonHTTPError(t *testing.T) {
	if isAuthHTTPError(errors.New("network down")) {
		t.Fatal("plain error should not be treated as auth HTTP error")
	}
}
