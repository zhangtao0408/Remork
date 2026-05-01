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

func TestInsecureNoTokenNonLoopbackListenAddrCases(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		hasToken bool
		want     bool
	}{
		{name: "wildcard ipv4", addr: "0.0.0.0:17731", want: true},
		{name: "wildcard ipv6", addr: "[::]:17731", want: true},
		{name: "expanded wildcard ipv6", addr: "[0:0:0:0:0:0:0:0]:17731", want: true},
		{name: "empty host wildcard", addr: ":17731", want: true},
		{name: "loopback ipv4", addr: "127.0.0.1:17731"},
		{name: "localhost", addr: "localhost:17731"},
		{name: "loopback ipv6", addr: "[::1]:17731"},
		{name: "wildcard with token", addr: "0.0.0.0:17731", hasToken: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := insecureNoTokenNonLoopbackListenAddr(tt.addr, tt.hasToken); got != tt.want {
				t.Fatalf("insecureNoTokenNonLoopbackListenAddr(%q, %t) = %t, want %t", tt.addr, tt.hasToken, got, tt.want)
			}
		})
	}
}
