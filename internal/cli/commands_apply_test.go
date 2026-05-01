package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"remork/internal/api"
	"remork/internal/apply"
	"remork/internal/state"
	"remork/internal/workspace"
)

func TestApplySurfacesPartialFailure(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "a.txt"), []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apply" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(apply.Result{
			Applied:    false,
			Partial:    []string{"a.txt"},
			FailedPath: "b.txt",
		}); err != nil {
			t.Errorf("encode result: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	cmd := NewRootCommand(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: fakeDaemonProbe{Roots: []string{"/data/project-a"}},
	})
	if _, err := executeCommand(cmd, "host", "add", "lab-a", "--url", server.URL); err != nil {
		t.Fatalf("host add: %v", err)
	}
	if _, err := executeCommand(cmd, "init", "lab-a:/data/project-a"); err != nil {
		t.Fatalf("init: %v", err)
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	snap := state.Snapshot{
		WorkspaceRef: "lab-a:/data/project-a",
		Entries: map[string]state.TrackedFile{
			"a.txt": {
				Path:     "a.txt",
				Type:     api.FileTypeFile,
				BaseHash: state.HashBytes([]byte("before")),
			},
		},
	}
	if err := state.NewStore(binding.StateDir).Save(snap); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "apply")
	if err == nil {
		t.Fatal("expected partial apply error")
	}
	got := out.String()
	for _, want := range []string{
		"apply failed at: b.txt",
		"changed paths: [a.txt]",
		"Run remork status and remork sync before retrying.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q: %q", want, got)
		}
	}

	out, err = executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "apply", "--json")
	if err == nil {
		t.Fatal("expected partial apply error for json")
	}
	var result apply.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode json output %q: %v", out.String(), err)
	}
	if result.Applied || result.FailedPath != "b.txt" || len(result.Partial) != 1 || result.Partial[0] != "a.txt" {
		t.Fatalf("json result = %#v", result)
	}
}
