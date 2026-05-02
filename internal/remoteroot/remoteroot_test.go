package remoteroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainsAllowsExactAndChildren(t *testing.T) {
	allowed, err := Normalize("/home/me/")
	if err != nil {
		t.Fatal(err)
	}

	for _, candidate := range []string{
		"/home/me",
		"/home/me/project",
		"/home/me/projects/a",
	} {
		ok, err := Contains([]Root{allowed}, candidate)
		if err != nil {
			t.Fatalf("Contains(%q): %v", candidate, err)
		}
		if !ok {
			t.Fatalf("Contains(%q) = false, want true", candidate)
		}
	}
}

func TestContainsRejectsSiblingPrefixAndRelativePaths(t *testing.T) {
	allowed, err := Normalize("/home/me")
	if err != nil {
		t.Fatal(err)
	}

	invalidCandidates := map[string]bool{
		"/home/me_other":   false,
		"/home/me/../root": false,
		"home/me":          true,
		"":                 true,
	}
	for candidate, wantErr := range invalidCandidates {
		ok, err := Contains([]Root{allowed}, candidate)
		if err == nil && ok {
			t.Fatalf("Contains(%q) = true, want false", candidate)
		}
		if wantErr && err == nil {
			t.Fatalf("Contains(%q) error = nil, want error", candidate)
		}
		if !wantErr && err != nil {
			t.Fatalf("Contains(%q) error = %v, want nil", candidate, err)
		}
	}
}

func TestNormalizeCleansTrailingSlash(t *testing.T) {
	root, err := Normalize("/home/me///")
	if err != nil {
		t.Fatal(err)
	}
	if root.Raw != "/home/me///" {
		t.Fatalf("Raw = %q, want original input", root.Raw)
	}
	if root.Clean != "/home/me" {
		t.Fatalf("Clean = %q, want /home/me", root.Clean)
	}
}

func TestContainsAllowsMultipleRoots(t *testing.T) {
	allowed, err := NormalizeMany([]string{"/opt/projects", "/home/me"})
	if err != nil {
		t.Fatal(err)
	}

	ok, err := Contains(allowed, "/home/me/project")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Contains() = false, want true")
	}

	ok, err = Contains(allowed, "/var/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Contains() = true, want false")
	}
}

func TestContainsRejectsInvalidAllowedRoot(t *testing.T) {
	ok, err := Contains([]Root{{}}, "/home/me")
	if err == nil {
		t.Fatal("Contains() error = nil, want invalid allowed root error")
	}
	if ok {
		t.Fatal("Contains() = true, want false")
	}
}

func TestContainsResolvedRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	allowedDir := filepath.Join(parent, "allowed")
	outsideDir := filepath.Join(parent, "outside")
	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(allowedDir, "link")); err != nil {
		t.Fatal(err)
	}
	allowed, err := Normalize(allowedDir)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := ContainsResolved([]Root{allowed}, filepath.Join(allowedDir, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("ContainsResolved() = true, want false for symlink escape")
	}
}

func TestContainsResolvedAllowsRealChild(t *testing.T) {
	allowedDir := t.TempDir()
	child := filepath.Join(allowedDir, "repo")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	allowed, err := Normalize(allowedDir)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := ContainsResolved([]Root{allowed}, child)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ContainsResolved() = false, want true")
	}

	canonical, ok, err := ResolveAllowed([]Root{allowed}, child+string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ResolveAllowed() ok = false, want true")
	}
	realChild, err := filepath.EvalSymlinks(child)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != realChild {
		t.Fatalf("ResolveAllowed() canonical = %q, want %q", canonical, realChild)
	}
}

func TestContainsResolvedRejectsSymlinkParentTraversalEscape(t *testing.T) {
	parent := t.TempDir()
	allowedDir := filepath.Join(parent, "allowed")
	targetDir := filepath.Join(parent, "outside", "target")
	secretDir := filepath.Join(parent, "outside", "secret")
	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(allowedDir, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, filepath.Join(allowedDir, "link")); err != nil {
		t.Fatal(err)
	}
	allowed, err := Normalize(allowedDir)
	if err != nil {
		t.Fatal(err)
	}

	candidate := allowedDir + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "secret"
	ok, err := ContainsResolved([]Root{allowed}, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("ContainsResolved() = true, want false for symlink parent traversal escape")
	}
}
