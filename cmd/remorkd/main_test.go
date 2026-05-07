package main

import (
	"os"
	"path/filepath"
	"testing"

	"remork/internal/remorkdconfig"
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

func TestServerConfigBuildsDaemonOptions(t *testing.T) {
	cfg := remorkdconfig.Config{
		ListenAddr:         "127.0.0.1:17731",
		AllowedRoots:       []string{"/data"},
		LargeFileThreshold: "128MB",
	}
	opts, err := serverOptionsFromConfig(cfg, "test")
	if err != nil {
		t.Fatalf("serverOptionsFromConfig: %v", err)
	}
	if opts.Addr != "127.0.0.1:17731" {
		t.Fatalf("addr = %q", opts.Addr)
	}
	if len(opts.Roots) != 1 || opts.Roots[0] != "/data" {
		t.Fatalf("roots = %#v", opts.Roots)
	}
	if opts.Version != "test" {
		t.Fatalf("version = %q", opts.Version)
	}
}

func TestRootFlagsAcceptRepeatedRoots(t *testing.T) {
	var roots rootFlags
	if err := roots.Set("/data"); err != nil {
		t.Fatalf("set first root: %v", err)
	}
	if err := roots.Set(" /scratch "); err != nil {
		t.Fatalf("set second root: %v", err)
	}
	if got, want := roots.String(), "/data,/scratch"; got != want {
		t.Fatalf("roots string = %q, want %q", got, want)
	}
	if len(roots) != 2 || roots[0] != "/data" || roots[1] != "/scratch" {
		t.Fatalf("roots = %#v", roots)
	}
}

func TestRootFlagsRejectEmptyRoot(t *testing.T) {
	var roots rootFlags
	if err := roots.Set(" \t"); err == nil {
		t.Fatal("empty root should be rejected")
	}
}
