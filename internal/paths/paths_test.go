package paths

import "testing"

func TestResolveInsideWorkspaceAcceptsCleanRelativePath(t *testing.T) {
	got, err := ResolveInsideWorkspace("/srv/project", "src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "/srv/project/src/main.go"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveInsideWorkspaceRejectsParentTraversal(t *testing.T) {
	_, err := ResolveInsideWorkspace("/srv/project", "../secret")
	if err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestResolveInsideWorkspaceRejectsAbsoluteEscape(t *testing.T) {
	_, err := ResolveInsideWorkspace("/srv/project", "/etc/passwd")
	if err == nil {
		t.Fatal("expected absolute escape error")
	}
}

func TestNormalizeRemotePathUsesSlashAndNoLeadingSlash(t *testing.T) {
	got, err := NormalizeRemotePath("./src//main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "src/main.go" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeRemotePathRejectsEmptyRootEscape(t *testing.T) {
	for _, input := range []string{"", ".", "..", "../x", "/x"} {
		if _, err := NormalizeRemotePath(input); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}
