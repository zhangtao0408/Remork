package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand("test-version")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "remork test-version" {
		t.Fatalf("output %q", out.String())
	}
}

func TestStatusCommandPrintsWorkspace(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand("test-version")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"status", "lab:/data/project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != "workspace lab:/data/project" {
		t.Fatalf("output %q", out.String())
	}
}
