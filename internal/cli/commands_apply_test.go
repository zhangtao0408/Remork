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
	"remork/internal/config"
	"remork/internal/limits"
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

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "apply", "--yes")
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

	out, err = executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "apply", "--yes", "--json")
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

func TestApplyJSONReportsPlanningErrors(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	if err := writeApplyTestBinding(home, local, state.Snapshot{WorkspaceRef: "lab:/data/project", Entries: map[string]state.TrackedFile{}}); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	file, err := os.Create(filepath.Join(local, "huge.bin"))
	if err != nil {
		t.Fatalf("create sparse file: %v", err)
	}
	if err := file.Truncate(limits.MaxApplyFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate sparse file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close sparse file: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "apply", "--include-untracked", "--yes", "--json")
	if err == nil {
		t.Fatal("expected large apply planning error")
	}
	var got commandErrorJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode json output %q: %v", out.String(), err)
	}
	if !strings.Contains(got.Error, "too large to apply") || got.Code == 0 {
		t.Fatalf("json error = %#v", got)
	}
}

func TestApplyJSONConflictUsesErrorEnvelope(t *testing.T) {
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
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(apply.Result{Applied: false, Conflicts: []string{"a.txt"}})
	}))
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: server.URL},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	binding := workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-test",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-test"),
	}
	if err := workspace.WriteBinding(local, binding); err != nil {
		t.Fatalf("write binding: %v", err)
	}
	if err := state.NewStore(binding.StateDir).Save(state.Snapshot{
		WorkspaceRef: "lab:/data/project",
		Entries: map[string]state.TrackedFile{
			"a.txt": {Path: "a.txt", Type: api.FileTypeFile, BaseHash: state.HashBytes([]byte("before"))},
		},
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "apply", "--yes", "--json")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	var got struct {
		Error     string   `json:"error"`
		Fix       string   `json:"fix"`
		Code      int      `json:"code"`
		Conflicts []string `json:"conflicts"`
	}
	if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil {
		t.Fatalf("decode json output %q: %v", out.String(), jsonErr)
	}
	if got.Code != 5 || !strings.Contains(got.Error, "conflict") || !strings.Contains(got.Fix, "remork conflict") || len(got.Conflicts) != 1 || got.Conflicts[0] != "a.txt" {
		t.Fatalf("json conflict = %#v", got)
	}
}

func TestApplyNonInteractiveRequiresYes(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "a.txt"), []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeApplyTestBinding(home, local, state.Snapshot{
		WorkspaceRef: "lab:/data/project",
		Entries: map[string]state.TrackedFile{
			"a.txt": {Path: "a.txt", Type: api.FileTypeFile, BaseHash: state.HashBytes([]byte("before"))},
		},
	}); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "apply", "--non-interactive")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("apply --non-interactive error = %v, output=%q; want --yes requirement", err, out.String())
	}
	if coded, ok := err.(interface{ ExitCode() int }); !ok || coded.ExitCode() != 7 {
		t.Fatalf("exit code = %v, want 7", err)
	}
}

func TestApplyJSONUsesEmptySkippedArray(t *testing.T) {
	result := applyJSONResult{
		ID:      "cs-test",
		Plan:    map[string]int{"create": 0, "update": 1, "delete": 0, "skipped": 0},
		Skipped: normalizeSkipped(nil),
		Applied: 1,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"skipped":null`) {
		t.Fatalf("skipped should be [], got %s", data)
	}
	if !strings.Contains(string(data), `"skipped":[]`) {
		t.Fatalf("missing skipped [] in %s", data)
	}
}

func writeApplyTestBinding(home, local string, snap state.Snapshot) error {
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: "http://127.0.0.1:1"},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		return err
	}
	binding := workspace.Binding{
		Version:     1,
		Host:        "lab",
		RemoteRoot:  "/data/project",
		WorkspaceID: "ws-test",
		StateDir:    filepath.Join(home, ".remork", "state", "ws-test"),
	}
	if err := workspace.WriteBinding(local, binding); err != nil {
		return err
	}
	return state.NewStore(binding.StateDir).Save(snap)
}
