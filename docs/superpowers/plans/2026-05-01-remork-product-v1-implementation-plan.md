# Remork Product V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver Remork Product V1 as a productized local CLI plus remote daemon workflow where users bind a local directory to a remote workspace, sync/edit/apply files safely, and run remote commands or shell sessions with clear state checks.

**Architecture:** Keep the existing Go monorepo and reuse the tested daemon, manifest, apply, planner, transfer, state, client, pty, watch, progress, and ops packages. Add product-level orchestration packages around CLI commands, workspace bindings, prompts, sync execution, diff rendering, apply changeset construction, run/shell preflight, daemon auth/status, and release validation. Preserve manifest reconciliation as the correctness source and keep remote operation logs scoped to `<workspace>/.remork/log/operations.jsonl`.

**Tech Stack:** Go 1.22+, Cobra CLI, stdlib `net/http`, Gorilla WebSocket, fsnotify, creack/pty, JSON local config/state for V1 with atomic writes, Go unit tests, Go e2e tests, shell-based remote smoke validation on Linux arm64 hosts using prebuilt `remorkd`.

---

## Current Baseline

The MVP already has these tested internal capabilities:

- `cmd/remorkd` starts a daemon for one root.
- `cmd/remork` has `version` and a placeholder `status`.
- `internal/daemon` exposes manifest, download, apply, exec, events, shell, and operations endpoints.
- `internal/client` calls manifest, download, apply, exec, and operations.
- `internal/manifest` scans workspaces, skips `.git` and `.remork`, and marks large files.
- `internal/planner` creates sync and pull operations.
- `internal/transfer` materializes files and `.meta` placeholders.
- `internal/state` saves snapshots and detects local dirty changes.
- `internal/apply` verifies base hashes and applies changes atomically.
- `internal/ops` stores per-workspace operation logs.

Product V1 should build on this baseline instead of replacing it.

## Product Decisions Locked For V1

- The taught command for non-interactive remote execution is `remork run`; `remork exec` is kept as a hidden compatibility alias.
- A local directory binding is the default workspace context after `remork init`.
- Daily commands are `init`, `sync`, `status`, `apply`, `run`, and `shell`.
- Advanced commands are `pull`, `diff`, `restore`, `log`, `watch`, `host`, and `workspace`.
- Debug and operations commands are `doctor`, `debug`, and `daemon`.
- V1 uses trusted VPN plus optional shared token, not accounts or RBAC.
- Token values are referenced through environment variables, not written directly into config.
- Large-file threshold remains `128MB`.
- Shell detach and attach are V1.1 goals; Product V1 must make the basic interactive shell stable, resize-aware, interruptible, and documented.

## Target File Structure

Create or modify these files:

```text
cmd/remork/main.go
cmd/remorkd/main.go
deploy/remorkd.example.toml
scripts/build-release.sh
scripts/remote-smoke.sh
README.md
docs/superpowers/specs/2026-05-01-remork-product-v1-design.md

internal/api/types.go
internal/auth/auth.go
internal/auth/auth_test.go
internal/client/client.go
internal/client/client_test.go
internal/cli/root.go
internal/cli/root_test.go
internal/cli/testutil_test.go
internal/cli/commands_host.go
internal/cli/commands_init.go
internal/cli/commands_sync.go
internal/cli/commands_status.go
internal/cli/commands_diff.go
internal/cli/commands_apply.go
internal/cli/commands_run.go
internal/cli/commands_shell.go
internal/cli/commands_pull.go
internal/cli/commands_log.go
internal/cli/commands_watch.go
internal/cli/commands_doctor.go
internal/cli/commands_debug.go
internal/cli/commands_daemon.go
internal/config/config.go
internal/config/config_test.go
internal/daemon/server.go
internal/daemon/server_test.go
internal/diff/diff.go
internal/diff/diff_test.go
internal/exitcode/exitcode.go
internal/output/json.go
internal/output/json_test.go
internal/preflight/preflight.go
internal/preflight/preflight_test.go
internal/prompt/prompt.go
internal/prompt/prompt_test.go
internal/shellclient/shellclient.go
internal/shellclient/shellclient_test.go
internal/syncer/syncer.go
internal/syncer/syncer_test.go
internal/workspace/binding.go
internal/workspace/binding_test.go
test/e2e/remork_cli_e2e_test.go
test/e2e/remork_conflict_e2e_test.go
test/e2e/remork_large_file_e2e_test.go
test/e2e/remork_shell_e2e_test.go
```

Responsibilities:

- `internal/auth`: shared-token request verification for daemon and token lookup for client.
- `internal/output`: JSON response helpers that keep machine JSON on stdout and human progress on stderr.
- `internal/exitcode`: stable product exit code categories.
- `internal/workspace`: local binding marker and current-directory workspace resolution.
- `internal/syncer`: orchestrates manifest, local state, planner, transfer, prompts, and progress.
- `internal/diff`: text diff and binary/large-file metadata diff rendering.
- `internal/preflight`: run/shell safe-mode checks.
- `internal/prompt`: interactive, force, quiet, and JSON-compatible confirmation policy.
- `internal/shellclient`: terminal WebSocket client for `/shell`, including resize and interrupt handling.
- `internal/cli`: Cobra commands and user-facing command text.
- `test/e2e`: product-level CLI workflows against an in-process daemon and temporary directories.

## Test Execution Rules

For every task:

1. Write or update the failing test first.
2. Run the narrow test and confirm it fails for the expected reason.
3. Implement the smallest code path that satisfies the test.
4. Run the narrow test and confirm it passes.
5. Run the package-level test for touched packages.
6. Commit with the exact message shown in the task.

Use these common commands:

```bash
go test ./internal/config ./internal/workspace ./internal/cli
go test ./internal/syncer ./internal/diff ./internal/preflight ./internal/prompt
go test ./internal/client ./internal/daemon ./internal/auth ./internal/shellclient
go test ./test/e2e -run TestRemorkProduct -count=1 -v
go test ./...
go test -race ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./test/e2e
```

## Task 1: Product CLI Shell, Help Layers, And Exit Codes

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`
- Create: `internal/cli/testutil_test.go`
- Create: `internal/exitcode/exitcode.go`
- Create: `internal/output/json.go`
- Create: `internal/output/json_test.go`

- [ ] **Step 1: Write failing tests for layered CLI help**

Add tests in `internal/cli/root_test.go`:

```go
func TestRootHelpShowsProductCommandLayers(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	mustContain(t, out, "Must know:")
	mustContain(t, out, "init")
	mustContain(t, out, "sync")
	mustContain(t, out, "status")
	mustContain(t, out, "apply")
	mustContain(t, out, "run")
	mustContain(t, out, "shell")
	mustContain(t, out, "Learn later:")
	mustContain(t, out, "pull")
	mustContain(t, out, "diff")
	mustContain(t, out, "restore")
	mustContain(t, out, "log")
	mustContain(t, out, "watch")
	mustContain(t, out, "Debug and operations:")
	mustContain(t, out, "doctor")
	mustContain(t, out, "debug")
	mustContain(t, out, "daemon")
}

func TestRunIsVisibleAndExecIsAlias(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	out, err := executeCommand(cmd, "help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	mustContain(t, out, "run")
	mustNotContain(t, out, "exec")

	execCmd, _, err := cmd.Find([]string{"exec"})
	if err != nil {
		t.Fatalf("find exec: %v", err)
	}
	if execCmd == nil {
		t.Fatal("exec alias command missing")
	}
	if !execCmd.Hidden {
		t.Fatal("exec alias must be hidden")
	}
}
```

Add helper functions in `internal/cli/testutil_test.go`:

```go
func executeCommand(cmd *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q\noutput:\n%s", want, got)
	}
}

func mustNotContain(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("expected output not to contain %q\noutput:\n%s", want, got)
	}
}
```

- [ ] **Step 2: Write failing tests for JSON output helper**

Create `internal/output/json_test.go`:

```go
func TestWriteJSONAddsTrailingNewline(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, map[string]string{"status": "ok"})
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if got := buf.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("json output = %q", got)
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/cli ./internal/output
```

Expected: fail because `Options`, layered help text, hidden `exec`, and `output.WriteJSON` do not exist.

- [ ] **Step 4: Implement exit code and output helpers**

Create `internal/exitcode/exitcode.go`:

```go
package exitcode

const (
	Success              = 0
	GeneralError         = 1
	InvalidUsageOrConfig = 2
	NetworkUnavailable   = 3
	LocalDirtyBlocked    = 4
	Conflict             = 5
	PermissionDenied     = 6
	PromptRequired       = 7
	RemoteCommandFailed  = 8
	Timeout              = 9
)
```

Create `internal/output/json.go`:

```go
package output

import (
	"encoding/json"
	"io"
)

func WriteJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}
```

- [ ] **Step 5: Refactor root command constructor**

Change `internal/cli/root.go` to expose an `Options` struct:

```go
type Options struct {
	Version string
}

func NewRootCommand(opts Options) *cobra.Command {
	if opts.Version == "" {
		opts.Version = "dev"
	}
	root := &cobra.Command{
		Use:          "remork",
		Short:        "Control remote workspaces from a local working copy",
		SilenceUsage: true,
	}
	root.SetHelpTemplate(productHelpTemplate)
	addVersionCommand(root, opts.Version)
	addPlaceholderProductCommands(root)
	return root
}
```

Add transitional command registration so help is correct while behavior lands in the following task groups:

```go
func addPlaceholderProductCommands(root *cobra.Command) {
	names := []string{"init", "sync", "status", "apply", "run", "shell", "pull", "diff", "restore", "log", "watch", "host", "workspace", "doctor", "debug", "daemon"}
	for _, name := range names {
		name := name
		root.AddCommand(&cobra.Command{
			Use:    name,
			Hidden: false,
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("%s command is defined by the Product V1 plan and has no handler in this task", name)
			},
		})
	}
	root.AddCommand(&cobra.Command{
		Use:    "exec",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("exec is a hidden alias for run and has no handler in this task")
		},
	})
}
```

Use a help template that includes:

```text
Must know:
  init        Bind the current directory to a remote workspace
  sync        Sync remote files into the local working copy
  status      Show local, remote, conflict, and large-file state
  apply       Write local changes to the remote after base checks
  run         Run a command in the remote workspace
  shell       Open an interactive remote shell

Learn later:
  pull        Fetch a specific file or directory
  diff        Show local changes against the synced base
  restore     Discard local changes
  log         Show recent remote Remork operations
  watch       Keep syncing from remote events
  host        Manage daemon endpoints
  workspace   Inspect or remove local bindings

Debug and operations:
  doctor      Check local and remote readiness
  debug       Inspect daemon APIs and events
  daemon      Install, upgrade, or inspect remorkd
```

- [ ] **Step 6: Update `cmd/remork/main.go`**

Change:

```go
if err := cli.NewRootCommand(cli.Options{Version: version}).Execute(); err != nil {
```

- [ ] **Step 7: Run tests and commit**

Run:

```bash
go test ./internal/cli ./internal/output
go test ./...
git add cmd/remork/main.go internal/cli internal/exitcode internal/output
git commit -m "feat: add product cli shell"
```

## Task 2: Host Config, Token References, And Workspace Binding

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Create: `internal/workspace/binding.go`
- Create: `internal/workspace/binding_test.go`
- Modify: `internal/cli/commands_host.go`
- Modify: `internal/cli/commands_init.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

- [ ] **Step 1: Write failing config tests**

Add to `internal/config/config_test.go`:

```go
func TestHostConfigStoresTokenReferenceAndNoProxy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cfg := Config{
		ClientID: "tao-macbook",
		Hosts: map[string]Host{
			"lab-a": {
				Name:     "lab-a",
				URL:      "http://remork-daemon.example.internal:17731",
				TokenEnv: "REMORK_LAB_A_TOKEN",
				NoProxy:  true,
			},
		},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := loaded.Hosts["lab-a"]
	if got.TokenEnv != "REMORK_LAB_A_TOKEN" || !got.NoProxy {
		t.Fatalf("host fields not preserved: %#v", got)
	}
}

func TestDefaultConfigWhenMissing(t *testing.T) {
	cfg, err := NewStore(t.TempDir()).LoadOrDefault()
	if err != nil {
		t.Fatalf("LoadOrDefault: %v", err)
	}
	if cfg.Hosts == nil || cfg.Workspaces == nil {
		t.Fatalf("maps must be initialized: %#v", cfg)
	}
}
```

- [ ] **Step 2: Write failing binding tests**

Create `internal/workspace/binding_test.go`:

```go
func TestWriteAndResolveBindingFromCurrentDirectory(t *testing.T) {
	local := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state", "ws_123")
	binding := Binding{
		Version:    1,
		Host:       "lab-a",
		RemoteRoot: "/data/project-a",
		WorkspaceID: "ws_123",
		StateDir:   stateDir,
	}
	if err := WriteBinding(local, binding); err != nil {
		t.Fatalf("WriteBinding: %v", err)
	}
	nested := filepath.Join(local, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, root, err := ResolveFrom(nested)
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if root != local {
		t.Fatalf("root = %q, want %q", root, local)
	}
	if got.Host != binding.Host || got.RemoteRoot != binding.RemoteRoot || got.StateDir != stateDir {
		t.Fatalf("binding mismatch: %#v", got)
	}
}

func TestBindingRejectsSecrets(t *testing.T) {
	err := WriteBinding(t.TempDir(), Binding{
		Version:    1,
		Host:       "lab-a",
		RemoteRoot: "/data/project-a",
		Token:      "secret",
	})
	if err == nil {
		t.Fatal("expected secret-bearing binding to be rejected")
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/config ./internal/workspace
```

Expected: fail because `ClientID`, `TokenEnv`, `NoProxy`, `LoadOrDefault`, and `workspace` package do not exist.

- [ ] **Step 4: Extend config model**

Modify `internal/config/config.go`:

```go
type Host struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	TokenEnv string `json:"token_env,omitempty"`
	NoProxy  bool   `json:"no_proxy,omitempty"`
}

type Config struct {
	ClientID   string               `json:"client_id,omitempty"`
	Hosts      map[string]Host      `json:"hosts"`
	Workspaces map[string]Workspace `json:"workspaces"`
}

func (s Store) LoadOrDefault() (Config, error) {
	cfg, err := s.Load()
	if os.IsNotExist(err) {
		return Config{Hosts: map[string]Host{}, Workspaces: map[string]Workspace{}}, nil
	}
	return cfg, err
}
```

Keep the existing JSON file name `config.json` for compatibility in V1. Add comments in README that the product design calls it config, while implementation currently uses JSON.

- [ ] **Step 5: Implement workspace binding**

Create `internal/workspace/binding.go`:

```go
package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const MarkerName = ".remork-local.json"

type Binding struct {
	Version     int    `json:"version"`
	Host        string `json:"host"`
	RemoteRoot  string `json:"remote_root"`
	WorkspaceID string `json:"workspace_id"`
	StateDir    string `json:"state_dir"`
	Token       string `json:"-"`
}

func WriteBinding(localRoot string, binding Binding) error {
	if binding.Token != "" {
		return errors.New("binding must not contain token secrets")
	}
	if binding.Version == 0 {
		binding.Version = 1
	}
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(localRoot, MarkerName+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(localRoot, MarkerName))
}

func ResolveFrom(start string) (Binding, string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return Binding{}, "", err
	}
	for {
		marker := filepath.Join(dir, MarkerName)
		data, err := os.ReadFile(marker)
		if err == nil {
			var binding Binding
			if err := json.Unmarshal(data, &binding); err != nil {
				return Binding{}, "", err
			}
			return binding, dir, nil
		}
		if !os.IsNotExist(err) {
			return Binding{}, "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Binding{}, "", errors.New("no remork workspace binding found")
		}
		dir = parent
	}
}
```

- [ ] **Step 6: Add host and init command tests**

Add CLI tests:

```go
func TestHostAddWritesConfig(t *testing.T) {
	home := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	_, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://remork-daemon.example.internal:17731", "--token-env", "REMORK_TOKEN", "--no-proxy")
	if err != nil {
		t.Fatalf("host add: %v", err)
	}
	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Hosts["lab-a"].TokenEnv != "REMORK_TOKEN" || !cfg.Hosts["lab-a"].NoProxy {
		t.Fatalf("host not saved: %#v", cfg.Hosts["lab-a"])
	}
}

func TestInitWritesLocalBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local, DaemonProbe: fakeDaemonProbe{Roots: []string{"/data/project-a"}}})
	_, err := executeCommand(cmd, "host", "add", "lab-a", "--url", "http://127.0.0.1:17731")
	if err != nil {
		t.Fatalf("host add: %v", err)
	}
	_, err = executeCommand(cmd, "init", "lab-a:/data/project-a")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if binding.Host != "lab-a" || binding.RemoteRoot != "/data/project-a" {
		t.Fatalf("binding mismatch: %#v", binding)
	}
}
```

- [ ] **Step 7: Implement host and init commands**

Add `Options` fields:

```go
type Options struct {
	Version    string
	HomeDir    string
	WorkingDir string
	DaemonProbe DaemonProbe
}
```

Register:

```go
addHostCommands(root, opts)
addInitCommand(root, opts)
```

`init` should parse `host:/absolute/path`, load host config, call a daemon status/root probe, create a stable workspace id from host plus remote root, create `~/.remork/state/<workspace-id>`, and write `.remork-local.json`.

- [ ] **Step 8: Run tests and commit**

Run:

```bash
go test ./internal/config ./internal/workspace ./internal/cli
go test ./...
git add internal/config internal/workspace internal/cli
git commit -m "feat: add host config and workspace binding"
```

## Task 3: Daemon Status, Shared Token Auth, And Client Headers

**Files:**
- Modify: `internal/api/types.go`
- Create: `internal/auth/auth.go`
- Create: `internal/auth/auth_test.go`
- Modify: `internal/client/client.go`
- Modify: `internal/client/client_test.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/daemon/server_test.go`
- Modify: `cmd/remorkd/main.go`
- Modify: `deploy/remorkd.example.toml`

- [ ] **Step 1: Write failing auth tests**

Create `internal/auth/auth_test.go`:

```go
func TestTokenFromEnv(t *testing.T) {
	t.Setenv("REMORK_TEST_TOKEN", "abc123")
	token, err := TokenFromEnv("REMORK_TEST_TOKEN")
	if err != nil {
		t.Fatalf("TokenFromEnv: %v", err)
	}
	if token != "abc123" {
		t.Fatalf("token = %q", token)
	}
}

func TestAuthorizeBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer abc123")
	if err := Authorize(req, "abc123"); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	req.Header.Set("Authorization", "Bearer wrong")
	if err := Authorize(req, "abc123"); err == nil {
		t.Fatal("expected wrong token to fail")
	}
}
```

- [ ] **Step 2: Write failing daemon status and auth tests**

Add to `internal/daemon/server_test.go`:

```go
func TestStatusReturnsVersionRootsAndThreshold(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Version: "test", Roots: []string{root}, LargeThreshold: 128 << 20}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d", resp.StatusCode)
	}
	var out api.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Version != "test" || out.Threshold != 128<<20 || len(out.Roots) != 1 || out.Roots[0] != root {
		t.Fatalf("bad status: %#v", out)
	}
}

func TestTokenProtectedManifestRejectsMissingToken(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}, Token: "secret"}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/manifest?root=" + url.QueryEscape(root) + "&path=.&recursive=true")
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status code = %d", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Write failing client header tests**

Add to `internal/client/client_test.go`:

```go
func TestClientSendsClientIDAndBearerToken(t *testing.T) {
	var gotClientID, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientID = r.Header.Get(api.HeaderClientID)
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(api.StatusResponse{Version: "test"})
	}))
	defer srv.Close()
	c := NewWithOptions(Options{BaseURL: srv.URL, ClientID: "tao-macbook", Token: "abc123"})
	if _, err := c.Status(); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if gotClientID != "tao-macbook" || gotAuth != "Bearer abc123" {
		t.Fatalf("headers client=%q auth=%q", gotClientID, gotAuth)
	}
}
```

- [ ] **Step 4: Run failing tests**

Run:

```bash
go test ./internal/auth ./internal/client ./internal/daemon
```

Expected: fail because auth package, `/status`, and `client.Status` do not exist.

- [ ] **Step 5: Implement auth and status**

Create `internal/auth/auth.go`:

```go
package auth

import (
	"errors"
	"net/http"
	"os"
	"strings"
)

var ErrUnauthorized = errors.New("unauthorized")

func TokenFromEnv(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	token := os.Getenv(name)
	if token == "" {
		return "", errors.New("token environment variable is empty: " + name)
	}
	return token, nil
}

func Authorize(r *http.Request, expected string) error {
	if expected == "" {
		return nil
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got != expected {
		return ErrUnauthorized
	}
	return nil
}
```

Extend daemon config:

```go
type Config struct {
	Version        string
	Roots          []string
	LargeThreshold int64
	Token          string
}
```

Add middleware inside `Server`:

```go
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if err := auth.Authorize(r, s.cfg.Token); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}
```

Call it at the start of all endpoints except `/status` if status is intentionally public. For Product V1, protect `/status` when a token is configured too, so host validation catches token mistakes.

Add:

```go
s.mux.HandleFunc("/status", s.handleStatus)
```

`handleStatus` returns `api.StatusResponse`.

- [ ] **Step 6: Implement client options**

Add:

```go
type Options struct {
	BaseURL  string
	ClientID string
	Token    string
	HTTP     *http.Client
}

func NewWithOptions(opts Options) Client {
	httpClient := opts.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return Client{base: opts.BaseURL, http: httpClient, clientID: opts.ClientID, token: opts.Token}
}
```

Add `Status()` and set authorization in `addHeaders`.

- [ ] **Step 7: Update daemon entrypoint**

Add `--token` and `--token-file` flags to `cmd/remorkd/main.go`. If both are set, fail with a clear error. If `--token-file` is set, read and trim the file.

- [ ] **Step 8: Run tests and commit**

Run:

```bash
go test ./internal/auth ./internal/client ./internal/daemon
go test ./...
git add internal/auth internal/api internal/client internal/daemon cmd/remorkd deploy/remorkd.example.toml
git commit -m "feat: add daemon status and token auth"
```

## Task 4: Sync Engine And `remork sync`

**Files:**
- Create: `internal/syncer/syncer.go`
- Create: `internal/syncer/syncer_test.go`
- Modify: `internal/state/state.go`
- Modify: `internal/state/state_test.go`
- Modify: `internal/transfer/transfer.go`
- Modify: `internal/transfer/transfer_test.go`
- Modify: `internal/cli/commands_sync.go`
- Modify: `internal/cli/root.go`
- Create: `test/e2e/remork_cli_e2e_test.go`

- [ ] **Step 1: Write failing syncer test for small files and large meta**

Create `internal/syncer/syncer_test.go`:

```go
func TestSyncMaterializesSmallFilesAndLargeMeta(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "src", "main.txt"), []byte("hello\n"))
	mustWriteFile(t, filepath.Join(remote, "model.tar.gz"), bytes.Repeat([]byte("x"), 8))
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 4}).Handler())
	defer srv.Close()

	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	runner := NewRunner(RunnerOptions{
		Client:       client.New(srv.URL),
		StateStore:   store,
		LocalRoot:    local,
		WorkspaceRef: "lab:" + remote,
		RemoteRoot:   remote,
	})
	result, err := runner.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.Downloaded != 1 || result.MetaWritten != 1 {
		t.Fatalf("result = %#v", result)
	}
	assertFileContent(t, filepath.Join(local, "src", "main.txt"), "hello\n")
	if _, err := os.Stat(filepath.Join(local, "model.tar.gz.meta")); err != nil {
		t.Fatalf("missing meta: %v", err)
	}
}
```

- [ ] **Step 2: Write failing syncer test for dirty preservation**

Add:

```go
func TestSyncPreservesDirtyLocalFile(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("remote-v1\n"))
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	defer srv.Close()
	runner := NewRunner(RunnerOptions{Client: client.New(srv.URL), StateStore: store, LocalRoot: local, WorkspaceRef: "lab:" + remote, RemoteRoot: remote})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	mustWriteFile(t, filepath.Join(local, "a.txt"), []byte("local-dirty\n"))
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("remote-v2\n"))
	result, err := runner.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if result.Conflicts != 1 {
		t.Fatalf("conflicts = %d", result.Conflicts)
	}
	assertFileContent(t, filepath.Join(local, "a.txt"), "local-dirty\n")
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/syncer
```

Expected: fail because `internal/syncer` does not exist.

- [ ] **Step 4: Implement syncer runner**

Create `internal/syncer/syncer.go` with:

```go
type RunnerOptions struct {
	Client       client.Client
	StateStore   state.Store
	LocalRoot    string
	WorkspaceRef string
	RemoteRoot   string
	Progress     ProgressReporter
}

type SyncOptions struct {
	TargetPath   string
	IncludeLarge bool
	Force        bool
	Quiet        bool
}

type Result struct {
	Downloaded  int `json:"downloaded"`
	MetaWritten int `json:"meta_written"`
	Deleted     int `json:"deleted"`
	Skipped     int `json:"skipped"`
	Conflicts   int `json:"conflicts"`
}
```

`Runner.Sync` algorithm:

1. Load snapshot with `StateStore.Load`; use empty snapshot when it does not exist.
2. Detect local dirty changes with `state.DetectDirty`.
3. Fetch manifest through client.
4. Build plan with `planner.PlanSync`.
5. For `OpDownload`, call `client.Download` and `transfer.WriteFile`.
6. For `OpWriteMeta`, call `manifest.BuildLargeMeta` and `transfer.WriteLargeMeta`.
7. For `OpDelete`, remove clean local file and matching base file if present.
8. For `OpConflict`, increment conflict count and leave local file untouched.
9. Save snapshot entries for successful materialization.

- [ ] **Step 5: Add `remork sync` CLI test**

In `test/e2e/remork_cli_e2e_test.go`, add:

```go
func TestRemorkProductSyncFromBoundDirectory(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("src/main.txt", "hello\n")
	h.run("host", "add", "lab", "--url", h.serverURL)
	h.runInLocal("init", "lab:"+h.remoteRoot)
	out := h.runInLocal("sync")
	mustContain(t, out, "downloaded 1")
	h.assertLocal("src/main.txt", "hello\n")
}
```

The harness should create temp home/local/remote directories, start `httptest.NewServer`, and invoke `cli.NewRootCommand` with injected home, working directory, and client factory.

- [ ] **Step 6: Implement `remork sync`**

`remork sync` should:

- Resolve current directory binding.
- Load host config.
- Create authenticated client.
- Construct syncer runner.
- Print a concise text summary:

```text
sync complete: downloaded 1, meta 0, deleted 0, conflicts 0
```

- Return exit code category `Conflict` when conflicts exist.
- Support `--json`, `--force`, and `--quiet`.

- [ ] **Step 7: Run tests and commit**

Run:

```bash
go test ./internal/syncer ./internal/cli ./test/e2e -run TestRemorkProductSyncFromBoundDirectory -count=1 -v
go test ./...
git add internal/syncer internal/state internal/transfer internal/cli test/e2e/remork_cli_e2e_test.go
git commit -m "feat: add sync command workflow"
```

## Task 5: `remork status --json` And Human State Summary

**Files:**
- Modify: `internal/syncer/syncer.go`
- Modify: `internal/syncer/syncer_test.go`
- Modify: `internal/cli/commands_status.go`
- Modify: `internal/cli/root.go`
- Modify: `test/e2e/remork_cli_e2e_test.go`

- [ ] **Step 1: Write failing status model tests**

Add to `internal/syncer/syncer_test.go`:

```go
func TestStatusReportsDirtyRemoteUpdatesConflictsAndLargePlaceholders(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("one\n"))
	mustWriteFile(t, filepath.Join(remote, "big.bin"), bytes.Repeat([]byte("x"), 8))
	store := state.NewStore(filepath.Join(t.TempDir(), "state"))
	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}, LargeThreshold: 4}).Handler())
	defer srv.Close()
	runner := NewRunner(RunnerOptions{Client: client.New(srv.URL), StateStore: store, LocalRoot: local, WorkspaceRef: "lab:" + remote, RemoteRoot: remote})
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	mustWriteFile(t, filepath.Join(local, "a.txt"), []byte("local\n"))
	mustWriteFile(t, filepath.Join(remote, "a.txt"), []byte("remote\n"))
	status, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.LocalChanges != 1 || status.Conflicts != 1 || status.LargePlaceholders != 1 {
		t.Fatalf("status = %#v", status)
	}
}
```

- [ ] **Step 2: Add failing CLI JSON status test**

In e2e:

```go
func TestRemorkProductStatusJSON(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "local\n")
	out := h.runInLocal("status", "--json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if got["local_changes"].(float64) != 1 {
		t.Fatalf("status json = %#v", got)
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/syncer ./test/e2e -run 'TestRemorkProductStatusJSON|TestRemorkProductSyncFromBoundDirectory' -count=1 -v
```

Expected: fail because `Runner.Status` and JSON output are missing.

- [ ] **Step 4: Implement status model**

Add:

```go
type Status struct {
	Workspace         string   `json:"workspace"`
	LocalRoot         string   `json:"local_root"`
	Clean             int      `json:"clean"`
	LocalChanges      int      `json:"local_changes"`
	RemoteUpdates     int      `json:"remote_updates"`
	Conflicts         int      `json:"conflicts"`
	LargePlaceholders int      `json:"large_placeholders"`
	ChangedPaths      []string `json:"changed_paths,omitempty"`
	ConflictPaths     []string `json:"conflict_paths,omitempty"`
}
```

`Runner.Status` should compare current manifest, snapshot, and dirty changes without writing files.

- [ ] **Step 5: Implement status CLI**

Text output must include:

```text
Workspace:
Local:
Clean:
Local changes:
Remote updates:
Conflicts:
Large placeholders:
Next:
```

`--json` must call `output.WriteJSON`.

- [ ] **Step 6: Run tests and commit**

Run:

```bash
go test ./internal/syncer ./internal/cli ./test/e2e -run TestRemorkProductStatusJSON -count=1 -v
go test ./...
git add internal/syncer internal/cli test/e2e/remork_cli_e2e_test.go
git commit -m "feat: add status command"
```

## Task 6: Diff And Restore Commands

**Files:**
- Create: `internal/diff/diff.go`
- Create: `internal/diff/diff_test.go`
- Modify: `internal/syncer/syncer.go`
- Modify: `internal/syncer/syncer_test.go`
- Modify: `internal/cli/commands_diff.go`
- Modify: `internal/cli/commands_restore.go`
- Modify: `internal/cli/root.go`
- Modify: `test/e2e/remork_cli_e2e_test.go`

- [ ] **Step 1: Write failing text diff tests**

Create `internal/diff/diff_test.go`:

```go
func TestUnifiedDiffShowsRemovedAndAddedLines(t *testing.T) {
	got := UnifiedText("a.txt", []byte("one\n"), []byte("two\n"))
	if !strings.Contains(got, "--- a.txt") || !strings.Contains(got, "+++ a.txt") {
		t.Fatalf("missing headers:\n%s", got)
	}
	if !strings.Contains(got, "-one") || !strings.Contains(got, "+two") {
		t.Fatalf("missing line changes:\n%s", got)
	}
}

func TestMetadataDiffForBinary(t *testing.T) {
	got := Metadata("model.bin", MetadataChange{OldSize: 10, NewSize: 12, Large: true})
	mustContainString(t, got, "model.bin")
	mustContainString(t, got, "binary or large file")
	mustContainString(t, got, "10 -> 12 bytes")
}
```

- [ ] **Step 2: Write failing e2e tests**

Add:

```go
func TestRemorkProductDiffAndRestore(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "two\n")
	diffOut := h.runInLocal("diff")
	mustContain(t, diffOut, "-one")
	mustContain(t, diffOut, "+two")
	h.runInLocal("restore", "a.txt")
	h.assertLocal("a.txt", "one\n")
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/diff ./test/e2e -run TestRemorkProductDiffAndRestore -count=1 -v
```

Expected: fail because `internal/diff`, `remork diff`, and `remork restore` do not exist.

- [ ] **Step 4: Implement diff rendering**

Implement a simple line-oriented unified diff in `internal/diff/diff.go`. V1 does not need an optimal Myers diff; use a deterministic whole-file fallback:

```go
func UnifiedText(path string, oldData, newData []byte) string {
	if bytes.Equal(oldData, newData) {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)
	for _, line := range splitLines(oldData) {
		fmt.Fprintf(&b, "-%s", line)
	}
	for _, line := range splitLines(newData) {
		fmt.Fprintf(&b, "+%s", line)
	}
	return b.String()
}
```

Add binary metadata rendering for non-text or large files.

- [ ] **Step 5: Implement base content cache if missing**

Ensure sync stores base content for normal files under:

```text
~/.remork/state/<workspace-id>/base/<relative-path>
```

Add `state.BasePath(stateDir, remotePath string) string` and tests for nested paths and path safety.

- [ ] **Step 6: Implement `diff` and `restore` commands**

`diff`:

- Resolve binding.
- Load dirty changes.
- For each dirty path, read base and local file.
- Render text or metadata diff.

`restore`:

- Resolve binding.
- Restore one path from base cache.
- `--all` restores all dirty paths.
- Do not fetch remote unless base cache is missing; if missing, return a clear error suggesting `remork sync --force`.

- [ ] **Step 7: Run tests and commit**

Run:

```bash
go test ./internal/diff ./internal/state ./internal/syncer ./test/e2e -run TestRemorkProductDiffAndRestore -count=1 -v
go test ./...
git add internal/diff internal/state internal/syncer internal/cli test/e2e/remork_cli_e2e_test.go
git commit -m "feat: add diff and restore commands"
```

## Task 7: Apply Changeset Builder And `remork apply`

**Files:**
- Modify: `internal/apply/apply.go`
- Modify: `internal/apply/apply_test.go`
- Create: `internal/syncer/apply.go`
- Modify: `internal/syncer/syncer_test.go`
- Modify: `internal/cli/commands_apply.go`
- Modify: `internal/cli/root.go`
- Create: `test/e2e/remork_conflict_e2e_test.go`

- [ ] **Step 1: Write failing changeset builder tests**

Add to `internal/syncer/syncer_test.go`:

```go
func TestBuildChangesetCreatesUpdatesDeletesAndSkipsLargeMeta(t *testing.T) {
	local := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	mustWriteFile(t, filepath.Join(local, "updated.txt"), []byte("new\n"))
	mustWriteFile(t, filepath.Join(local, "created.txt"), []byte("created\n"))
	mustWriteFile(t, filepath.Join(local, "big.bin.meta"), []byte("{}"))
	snap := state.Snapshot{
		WorkspaceRef: "lab:/remote",
		Entries: map[string]state.TrackedFile{
			"updated.txt": {Path: "updated.txt", BaseHash: state.HashBytes([]byte("old\n")), Revision: "r1"},
			"deleted.txt": {Path: "deleted.txt", BaseHash: state.HashBytes([]byte("gone\n")), Revision: "r2"},
			"big.bin":     {Path: "big.bin", Large: true, MetaPath: "big.bin.meta", Revision: "r3"},
		},
	}
	changes, skipped, err := BuildChangeset(local, snap)
	if err != nil {
		t.Fatalf("BuildChangeset: %v", err)
	}
	if len(changes.Changes) != 3 {
		t.Fatalf("changes = %#v skipped=%#v", changes.Changes, skipped)
	}
	if !containsSkipped(skipped, "big.bin.meta") {
		t.Fatalf("large meta edit not skipped: %#v", skipped)
	}
}
```

- [ ] **Step 2: Write failing apply e2e test**

Create `test/e2e/remork_conflict_e2e_test.go`:

```go
func TestRemorkProductApplyUpdatesRemoteAndConflictPreservesLocal(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "base\n")
	h.bindAndSync()
	h.writeLocal("a.txt", "local\n")
	applyOut := h.runInLocal("apply")
	mustContain(t, applyOut, "applied 1")
	h.assertRemote("a.txt", "local\n")

	h.writeLocal("a.txt", "local-two\n")
	h.writeRemote("a.txt", "remote-two\n")
	errOut, code := h.runInLocalExpectCode(5, "apply")
	mustContain(t, errOut, "conflict")
	h.assertLocal("a.txt", "local-two\n")
	h.assertRemote("a.txt", "remote-two\n")
	if code != 5 {
		t.Fatalf("exit code = %d", code)
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/syncer ./test/e2e -run TestRemorkProductApplyUpdatesRemoteAndConflictPreservesLocal -count=1 -v
```

Expected: fail because changeset builder and apply command are missing.

- [ ] **Step 4: Implement changeset builder**

Create `internal/syncer/apply.go`:

```go
type SkippedChange struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func BuildChangeset(localRoot string, snap state.Snapshot) (apply.Changeset, []SkippedChange, error)
```

Rules:

- Existing tracked file modified: `apply.ChangeUpdate` with tracked base hash and local content.
- Existing tracked file deleted: `apply.ChangeDelete` with tracked base hash.
- New local file: `apply.ChangeCreate` with local content.
- `.remork-local.json`, `.git`, `.remork`, and `*.meta` placeholder edits are skipped.
- Sort changes by path for deterministic output.
- Assign a changeset ID with timestamp plus hash of path/kind/base/content metadata.

- [ ] **Step 5: Implement apply CLI**

`remork apply` should:

- Resolve binding and host.
- Build changeset.
- Print dry-run plan for `--dry-run`.
- On success, run a targeted sync or update local snapshot/base cache.
- On conflict, print paths and return exit code `5`.
- Support `--json`.

Expected text output:

```text
apply plan: create 1, update 1, delete 1, skipped 1
applied 3
```

- [ ] **Step 6: Run tests and commit**

Run:

```bash
go test ./internal/apply ./internal/syncer ./test/e2e -run TestRemorkProductApplyUpdatesRemoteAndConflictPreservesLocal -count=1 -v
go test ./...
git add internal/apply internal/syncer internal/cli test/e2e/remork_conflict_e2e_test.go
git commit -m "feat: add apply command workflow"
```

## Task 8: Pull Command, Large File Prompts, And Progress Policy

**Files:**
- Modify: `internal/prompt/prompt.go`
- Modify: `internal/prompt/prompt_test.go`
- Modify: `internal/syncer/syncer.go`
- Modify: `internal/syncer/syncer_test.go`
- Modify: `internal/progress/progress.go`
- Modify: `internal/progress/progress_test.go`
- Modify: `internal/cli/commands_pull.go`
- Modify: `internal/cli/root.go`
- Create: `test/e2e/remork_large_file_e2e_test.go`

- [ ] **Step 1: Write failing prompt policy tests**

Create `internal/prompt/prompt_test.go`:

```go
func TestQuietReturnsPromptRequired(t *testing.T) {
	_, err := Confirm(Options{Quiet: true}, "download 200MB file?")
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestForceConfirmsWithoutReadingInput(t *testing.T) {
	ok, err := Confirm(Options{Force: true, In: strings.NewReader("")}, "replace file?")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !ok {
		t.Fatal("force should confirm")
	}
}

func TestInteractiveAcceptsY(t *testing.T) {
	var out bytes.Buffer
	ok, err := Confirm(Options{In: strings.NewReader("Y\n"), Out: &out}, "download file?")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !strings.Contains(out.String(), "download file?") {
		t.Fatalf("prompt output = %q", out.String())
	}
}
```

- [ ] **Step 2: Write failing large-file e2e tests**

Create `test/e2e/remork_large_file_e2e_test.go`:

```go
func TestRemorkProductLargeFilePullPolicies(t *testing.T) {
	h := newProductHarnessWithThreshold(t, 4)
	h.writeRemoteBytes("model.tar.gz", bytes.Repeat([]byte("x"), 8))
	h.bindAndSync()
	if _, err := os.Stat(filepath.Join(h.localRoot, "model.tar.gz.meta")); err != nil {
		t.Fatalf("missing meta: %v", err)
	}
	out, code := h.runInLocalExpectCode(7, "pull", "--quiet", "model.tar.gz")
	mustContain(t, out, "confirmation required")
	if code != 7 {
		t.Fatalf("exit code = %d", code)
	}
	h.runInLocal("pull", "--force", "model.tar.gz")
	got, err := os.ReadFile(filepath.Join(h.localRoot, "model.tar.gz"))
	if err != nil {
		t.Fatalf("read pulled: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("pulled size = %d", len(got))
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/prompt ./test/e2e -run TestRemorkProductLargeFilePullPolicies -count=1 -v
```

Expected: fail because prompt package and pull command are missing.

- [ ] **Step 4: Implement prompt policy**

Create:

```go
var ErrPromptRequired = errors.New("confirmation required")

type Options struct {
	Force bool
	Quiet bool
	In    io.Reader
	Out   io.Writer
}

func Confirm(opts Options, message string) (bool, error)
```

Behavior:

- `Force`: return `true, nil`.
- `Quiet`: return `false, ErrPromptRequired`.
- Interactive: print `<message> [y/N] ` and accept `y` or `Y`.

- [ ] **Step 5: Extend syncer for pull**

Add:

```go
func (r Runner) Pull(ctx context.Context, target string, opts PullOptions) (Result, error)
```

Rules:

- Fetch manifest for target path or full manifest then filter if daemon manifest does not support target files precisely.
- Use `planner.PlanPull`.
- For large file and `IncludeLarge == true`, call prompt unless `Force`.
- `--quiet` returns `prompt.ErrPromptRequired` when confirmation is needed.
- Full pulled file replaces `.meta` and snapshot marks pulled state.

- [ ] **Step 6: Implement pull CLI**

`remork pull <path>` supports `--force`, `--quiet`, `--json`, and `--include-large` as a hidden compatibility flag. The taught behavior is that a direct file pull of a large file asks or uses `--force`.

- [ ] **Step 7: Run tests and commit**

Run:

```bash
go test ./internal/prompt ./internal/syncer ./internal/progress ./test/e2e -run TestRemorkProductLargeFilePullPolicies -count=1 -v
go test ./...
git add internal/prompt internal/syncer internal/progress internal/cli test/e2e/remork_large_file_e2e_test.go
git commit -m "feat: add pull command and prompt policy"
```

## Task 9: Run Safe Mode And Hidden Exec Alias

**Files:**
- Create: `internal/preflight/preflight.go`
- Create: `internal/preflight/preflight_test.go`
- Modify: `internal/client/client.go`
- Modify: `internal/cli/commands_run.go`
- Modify: `internal/cli/root.go`
- Modify: `test/e2e/remork_cli_e2e_test.go`

- [ ] **Step 1: Write failing preflight tests**

Create `internal/preflight/preflight_test.go`:

```go
func TestRunPreflightBlocksDirtyWorkspace(t *testing.T) {
	decision := Decide(WorkspaceState{LocalDirty: 1, RemoteStale: false}, Options{})
	if decision.Allow {
		t.Fatalf("expected blocked decision: %#v", decision)
	}
	if decision.ExitCode != exitcode.LocalDirtyBlocked {
		t.Fatalf("exit = %d", decision.ExitCode)
	}
	if !strings.Contains(decision.Message, "remork apply") {
		t.Fatalf("message = %q", decision.Message)
	}
}

func TestRunPreflightAllowsRemoteOnly(t *testing.T) {
	decision := Decide(WorkspaceState{LocalDirty: 1, RemoteStale: true}, Options{RemoteOnly: true})
	if !decision.Allow {
		t.Fatalf("expected allow: %#v", decision)
	}
	if !strings.Contains(decision.Warning, "local pending changes are ignored") {
		t.Fatalf("warning = %q", decision.Warning)
	}
}
```

- [ ] **Step 2: Write failing run e2e tests**

Add:

```go
func TestRemorkProductRunSafeMode(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "remote\n")
	h.bindAndSync()
	out := h.runInLocal("run", "cat a.txt")
	mustContain(t, out, "remote")

	h.writeLocal("a.txt", "local\n")
	blocked, code := h.runInLocalExpectCode(4, "run", "cat a.txt")
	mustContain(t, blocked, "Local changes exist")
	if code != 4 {
		t.Fatalf("exit code = %d", code)
	}

	remoteOnly := h.runInLocal("run", "--remote-only", "cat a.txt")
	mustContain(t, remoteOnly, "remote")
	mustContain(t, remoteOnly, "local pending changes are ignored")
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/preflight ./test/e2e -run TestRemorkProductRunSafeMode -count=1 -v
```

Expected: fail because preflight and run command are missing.

- [ ] **Step 4: Implement preflight**

Create:

```go
type WorkspaceState struct {
	LocalDirty int
	RemoteStale bool
	Conflicts int
}

type Options struct {
	RemoteOnly  bool
	NoSyncCheck bool
}

type Decision struct {
	Allow    bool
	ExitCode int
	Message  string
	Warning  string
}
```

`Decide` blocks dirty/conflict state unless `RemoteOnly` or `NoSyncCheck` is set. `RemoteOnly` must return a visible warning.

- [ ] **Step 5: Implement run command**

Parsing rules:

- `remork run "pytest -q"` runs through `sh -c "pytest -q"`.
- `remork run -- python train.py --epochs 1` runs exact argv.
- Hidden `remork exec` uses the same handler.

Behavior:

- Resolve binding.
- Build workspace status.
- Apply preflight unless `--remote-only` or `--no-sync-check`.
- Call `client.Exec`.
- Stream or print stdout and stderr.
- Return exit code `8` when the remote command exits non-zero.
- Return exit code `9` when timed out.

- [ ] **Step 6: Run tests and commit**

Run:

```bash
go test ./internal/preflight ./internal/cli ./test/e2e -run TestRemorkProductRunSafeMode -count=1 -v
go test ./...
git add internal/preflight internal/client internal/cli test/e2e/remork_cli_e2e_test.go
git commit -m "feat: add run command safe mode"
```

## Task 10: Interactive Shell Client

**Files:**
- Create: `internal/shellclient/shellclient.go`
- Create: `internal/shellclient/shellclient_test.go`
- Modify: `internal/pty/session.go`
- Modify: `internal/pty/session_test.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/daemon/server_test.go`
- Modify: `internal/cli/commands_shell.go`
- Modify: `internal/cli/root.go`
- Create: `test/e2e/remork_shell_e2e_test.go`

- [ ] **Step 1: Write failing daemon shell resize test**

Add to `internal/daemon/server_test.go`:

```go
func TestShellAcceptsResizeFrame(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	root := t.TempDir()
	srv := httptest.NewServer(NewServer(Config{Roots: []string{root}}).Handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell?root=" + url.QueryEscape(root)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	frame := api.ShellFrame{Type: "resize", Rows: 30, Cols: 100}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("resize: %v", err)
	}
}
```

- [ ] **Step 2: Write failing shell client tests**

Create `internal/shellclient/shellclient_test.go`:

```go
func TestBuildShellURLIncludesRoot(t *testing.T) {
	got := BuildURL("http://127.0.0.1:17731", "/data/project")
	if got != "ws://127.0.0.1:17731/shell?root=%2Fdata%2Fproject" {
		t.Fatalf("url = %q", got)
	}
}
```

- [ ] **Step 3: Write failing shell e2e smoke**

Create `test/e2e/remork_shell_e2e_test.go`:

```go
func TestRemorkProductShellRemoteOnlySmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pty shell is not supported on windows")
	}
	h := newProductHarness(t)
	h.writeRemote("a.txt", "shell\n")
	h.bindAndSync()
	out := h.runShellScript("shell", "--remote-only", "printf 'cat a.txt\\nexit\\n'")
	mustContain(t, out, "shell")
	mustContain(t, out, "Remote-only shell")
}
```

- [ ] **Step 4: Run failing tests**

Run:

```bash
go test ./internal/shellclient ./internal/daemon ./test/e2e -run TestRemorkProductShellRemoteOnlySmoke -count=1 -v
```

Expected: fail because shell frames and shell CLI are missing.

- [ ] **Step 5: Add shell frame type**

In `internal/api/types.go`:

```go
type ShellFrame struct {
	Type string `json:"type"`
	Data []byte `json:"data,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Cols int    `json:"cols,omitempty"`
}
```

Daemon `/shell` should continue accepting raw binary/text for compatibility and also accept JSON resize frames. Resize frames call `pty.Session.Resize(rows, cols)`.

- [ ] **Step 6: Implement shell client**

`shellclient.Run` should:

- Dial `/shell?root=...`.
- Copy local stdin to WebSocket frames.
- Copy WebSocket output to local stdout.
- Watch terminal resize where supported.
- Send a resize frame on start.
- Return when the socket closes or context is canceled.

Tests can use injected readers/writers and a fake WebSocket server for deterministic behavior.

- [ ] **Step 7: Implement shell CLI**

`remork shell`:

- Resolves binding.
- Runs preflight using same rules as `run`.
- Prints remote-only warning when set.
- Starts `shellclient.Run`.
- After shell exits, fetch manifest/status; if remote revision changed, print `Remote workspace changed during shell session. Run remork sync to update local files.`

- [ ] **Step 8: Run tests and commit**

Run:

```bash
go test ./internal/shellclient ./internal/daemon ./internal/pty ./test/e2e -run TestRemorkProductShellRemoteOnlySmoke -count=1 -v
go test ./...
git add internal/api internal/shellclient internal/pty internal/daemon internal/cli test/e2e/remork_shell_e2e_test.go
git commit -m "feat: add shell command client"
```

## Task 11: Log, Watch, Doctor, And Debug Commands

**Files:**
- Modify: `internal/cli/commands_log.go`
- Modify: `internal/cli/commands_watch.go`
- Modify: `internal/cli/commands_doctor.go`
- Modify: `internal/cli/commands_debug.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/client/client.go`
- Modify: `internal/client/client_test.go`
- Modify: `test/e2e/remork_cli_e2e_test.go`

- [ ] **Step 1: Write failing log e2e test**

Add:

```go
func TestRemorkProductLogShowsWorkspaceOperations(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	h.runInLocal("run", "cat a.txt")
	out := h.runInLocal("log", "--limit", "5")
	mustContain(t, out, "run")
	mustContain(t, out, "tao-test")
}
```

- [ ] **Step 2: Write failing doctor e2e test**

Add:

```go
func TestRemorkProductDoctorReportsReady(t *testing.T) {
	h := newProductHarness(t)
	h.writeRemote("a.txt", "one\n")
	h.bindAndSync()
	out := h.runInLocal("doctor")
	mustContain(t, out, "OK: workspace is ready")
}
```

- [ ] **Step 3: Write failing debug command tests**

In `internal/cli/root_test.go`:

```go
func TestDebugCommandsAreRegistered(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	for _, args := range [][]string{{"debug", "manifest"}, {"debug", "events"}, {"debug", "api"}} {
		found, _, err := cmd.Find(args)
		if err != nil || found == nil {
			t.Fatalf("command %v not registered: %v", args, err)
		}
	}
}
```

- [ ] **Step 4: Run failing tests**

Run:

```bash
go test ./internal/cli ./test/e2e -run 'TestRemorkProductLogShowsWorkspaceOperations|TestRemorkProductDoctorReportsReady' -count=1 -v
```

Expected: fail because log, doctor, and debug commands are missing.

- [ ] **Step 5: Implement log**

`remork log` should call `client.Operations(root, limit)` and render:

```text
time                  client      operation  result   summary
2026-05-01T10:00:00Z  tao-test    run        success  cat a.txt
```

`--json` writes raw entries.

- [ ] **Step 6: Implement doctor**

Checks:

- Config file readable.
- Current directory binding exists.
- Host exists.
- Token environment variable present when configured.
- Daemon `/status` reachable.
- Remote root is listed in status roots.
- Manifest request succeeds.
- Operation log endpoint succeeds.

Output ends with `OK: workspace is ready` or `FAILED: <reason>` plus `Fix: <specific command or action>`.

- [ ] **Step 7: Implement watch and debug commands**

`remork watch`:

- Connects `/events`.
- Prints event summaries.
- Calls manifest reconciliation on connect, overflow, reconnect, and revision gap.
- In V1, keep it foreground only.

`remork debug manifest`:

- Calls manifest and prints either JSON or a summary of path/type/large/hash fields.

`remork debug events`:

- Connects events and prints normalized event JSON lines.

`remork debug api`:

- Calls `/status`, `/manifest`, and `/operations`, printing status code, latency, and concise result.

- [ ] **Step 8: Run tests and commit**

Run:

```bash
go test ./internal/cli ./internal/client ./test/e2e -run 'TestRemorkProductLogShowsWorkspaceOperations|TestRemorkProductDoctorReportsReady' -count=1 -v
go test ./...
git add internal/cli internal/client test/e2e/remork_cli_e2e_test.go
git commit -m "feat: add observability commands"
```

## Task 12: Daemon Install Helpers, Release Packaging, And README Product Rewrite

**Files:**
- Modify: `scripts/build-release.sh`
- Create: `scripts/remote-smoke.sh`
- Modify: `deploy/remorkd.example.toml`
- Modify: `internal/cli/commands_daemon.go`
- Modify: `internal/cli/root.go`
- Modify: `README.md`
- Create: `dist/README-release.md` during build only, not committed
- Modify: `.gitignore` if release scratch files are added

- [ ] **Step 1: Write failing release script smoke test**

Create a shell check command in the plan executor notes and run it manually because this repo does not yet have shell test harness:

```bash
scripts/build-release.sh dev
test -x dist/remork-darwin-arm64
test -x dist/remorkd-linux-arm64
test -f dist/checksums.txt
test -f dist/remorkd.example.toml
test -f dist/README-release.md
```

Expected before implementation: fail because some artifacts are missing.

- [ ] **Step 2: Write failing daemon command tests**

Add to `internal/cli/root_test.go`:

```go
func TestDaemonCommandsAreRegistered(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	for _, args := range [][]string{{"daemon", "install"}, {"daemon", "upgrade"}, {"daemon", "status"}} {
		found, _, err := cmd.Find(args)
		if err != nil || found == nil {
			t.Fatalf("command %v not registered: %v", args, err)
		}
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/cli
scripts/build-release.sh dev
```

Expected: CLI registration and release artifact checks fail until implemented.

- [ ] **Step 4: Update build script**

Build these artifacts:

```text
dist/remork-darwin-arm64
dist/remork-darwin-amd64
dist/remork-linux-arm64
dist/remork-linux-amd64
dist/remorkd-darwin-arm64
dist/remorkd-darwin-amd64
dist/remorkd-linux-arm64
dist/remorkd-linux-amd64
dist/remorkd.example.toml
dist/checksums.txt
dist/README-release.md
```

Use `CGO_ENABLED=0` for Linux builds where dependencies allow it.

- [ ] **Step 5: Implement daemon commands**

`remork daemon status <host>`:

- Loads host config.
- Calls `/status`.
- Prints daemon version, platform, roots, threshold, and auth state.

`remork daemon install` and `upgrade`:

- Product V1 implements SSH-based automation for install and upgrade when SSH is available.
- If SSH execution is not configured, print exact manual `scp` and remote start commands.
- Never require remote Go, npm, apt, brew, or internet.

- [ ] **Step 6: Rewrite README learning path**

README should have these sections in order:

```text
What Remork is
Five-minute workflow
The six commands you must know
Daily examples
Large files
Applying safely
Running commands and shell
Operation log
Advanced commands
Debug and maintenance commands
Offline daemon deployment
Safety model and limitations
Developer API notes
```

Keep API routes after user workflow sections.

- [ ] **Step 7: Run tests and commit**

Run:

```bash
go test ./internal/cli
scripts/build-release.sh dev
go test ./...
git add scripts/build-release.sh scripts/remote-smoke.sh deploy/remorkd.example.toml internal/cli README.md .gitignore
git commit -m "docs: productize release and readme"
```

## Task 13: Product E2E Suite And Race Validation

**Files:**
- Modify: `test/e2e/remork_cli_e2e_test.go`
- Modify: `test/e2e/remork_conflict_e2e_test.go`
- Modify: `test/e2e/remork_large_file_e2e_test.go`
- Modify: `test/e2e/remork_shell_e2e_test.go`
- Modify: `internal/cli/testutil_test.go`

- [ ] **Step 1: Add one full workflow e2e test**

Add:

```go
func TestRemorkProductFullWorkflow(t *testing.T) {
	h := newProductHarnessWithThreshold(t, 4)
	h.writeRemote("src/main.txt", "hello\n")
	h.writeRemoteBytes("model.tar.gz", bytes.Repeat([]byte("x"), 8))
	h.run("host", "add", "lab", "--url", h.serverURL)
	h.runInLocal("init", "lab:"+h.remoteRoot)
	h.runInLocal("sync")
	h.assertLocal("src/main.txt", "hello\n")
	if _, err := os.Stat(filepath.Join(h.localRoot, "model.tar.gz.meta")); err != nil {
		t.Fatalf("large meta missing: %v", err)
	}
	h.writeLocal("src/main.txt", "hello product\n")
	mustContain(t, h.runInLocal("status"), "Local changes: 1")
	mustContain(t, h.runInLocal("diff"), "+hello product")
	h.runInLocal("apply")
	mustContain(t, h.runInLocal("run", "cat src/main.txt"), "hello product")
	h.runInLocal("pull", "--force", "model.tar.gz")
	mustContain(t, h.runInLocal("log"), "apply")
}
```

- [ ] **Step 2: Run the failing full workflow test**

Run:

```bash
go test ./test/e2e -run TestRemorkProductFullWorkflow -count=1 -v
```

Expected: fail until all previous tasks are integrated.

- [ ] **Step 3: Fix integration defects only**

At this stage, do not add new product scope. Fix wiring errors, stale snapshots, command output mismatch, and test harness issues discovered by the full workflow.

- [ ] **Step 4: Run full local validation**

Run:

```bash
go test ./...
go test ./test/e2e -run TestRemorkProduct -count=3 -v
go test -race ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./test/e2e
scripts/build-release.sh dev
```

Expected: all pass.

- [ ] **Step 5: Commit**

Run:

```bash
git add test/e2e internal/cli
git commit -m "test: add product e2e validation"
```

## Task 14: Remote Validation On Linux Arm64 Hosts

**Files:**
- Modify: `scripts/remote-smoke.sh`
- Modify: `README.md`
- Create: `docs/remork-product-v1-validation.md`

- [ ] **Step 1: Build release locally**

Run:

```bash
scripts/build-release.sh dev
test -x dist/remork
test -x dist/remorkd-linux-arm64
```

Expected: release build succeeds.

- [ ] **Step 2: Validate `remork-host-a`**

Run:

```bash
scp dist/remorkd-linux-arm64 remork-host-a:/tmp/remorkd
ssh remork-host-a 'chmod +x /tmp/remorkd && rm -rf /tmp/remork-e2e && mkdir -p /tmp/remork-e2e && printf "hello\n" >/tmp/remork-e2e/a.txt'
ssh remork-host-a 'nohup /tmp/remorkd --root /tmp/remork-e2e --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo $! >/tmp/remorkd.pid'
curl --noproxy '*' -fsS 'http://remork-daemon-a.example.internal:17731/status'
curl --noproxy '*' -fsS 'http://remork-daemon-a.example.internal:17731/manifest?root=/tmp/remork-e2e&path=.&recursive=true'
```

Then run local CLI workflow from a temporary local directory:

```bash
tmp="$(mktemp -d)"
dist/remork host add z7 --url http://remork-daemon-a.example.internal:17731 --no-proxy
cd "$tmp"
dist/remork init z7:/tmp/remork-e2e
dist/remork sync
printf "from-remork\n" > a.txt
dist/remork apply
dist/remork run "cat a.txt"
dist/remork log --limit 10
```

Expected:

- `sync` downloads `a.txt`.
- `apply` updates remote.
- `run` prints `from-remork`.
- `log` shows `apply` and `run`.
- Remote operation log exists at `/tmp/remork-e2e/.remork/log/operations.jsonl`.

- [ ] **Step 3: Validate `remork-host-b`**

Run equivalent commands with:

```text
host alias: remork-host-b
VPN URL: http://remork-daemon-b.example.internal:17731
```

Expected: same outcome as `remork-host-a`.

- [ ] **Step 4: Cleanup both remotes**

Run:

```bash
ssh remork-host-a 'if [ -f /tmp/remorkd.pid ]; then kill "$(cat /tmp/remorkd.pid)" 2>/dev/null || true; fi; rm -rf /tmp/remorkd /tmp/remorkd.pid /tmp/remorkd.log /tmp/remork-e2e'
ssh remork-host-b 'if [ -f /tmp/remorkd.pid ]; then kill "$(cat /tmp/remorkd.pid)" 2>/dev/null || true; fi; rm -rf /tmp/remorkd /tmp/remorkd.pid /tmp/remorkd.log /tmp/remork-e2e'
```

- [ ] **Step 5: Record validation evidence**

Create `docs/remork-product-v1-validation.md`:

```markdown
# Remork Product V1 Validation

Date: 2026-05-01

## Local

- `go test ./...`: PASS
- `go test -race ./internal/daemon ./internal/client ./internal/syncer ./internal/preflight ./test/e2e`: PASS
- `scripts/build-release.sh dev`: PASS

## Remote Hosts

### remork-host-a

- Copied `dist/remorkd-linux-arm64` to `/tmp/remorkd`.
- Started daemon with `/tmp/remork-e2e` root.
- Verified direct VPN HTTP with `curl --noproxy '*'`.
- Ran `init`, `sync`, `apply`, `run`, and `log`.
- Verified operation log under `/tmp/remork-e2e/.remork/log/operations.jsonl`.
- Cleaned `/tmp/remorkd*` and `/tmp/remork-e2e`.

### remork-host-b

- Copied `dist/remorkd-linux-arm64` to `/tmp/remorkd`.
- Started daemon with `/tmp/remork-e2e` root.
- Verified direct VPN HTTP with `curl --noproxy '*'`.
- Ran `init`, `sync`, `apply`, `run`, and `log`.
- Verified operation log under `/tmp/remork-e2e/.remork/log/operations.jsonl`.
- Cleaned `/tmp/remorkd*` and `/tmp/remork-e2e`.
```

- [ ] **Step 6: Commit**

Run:

```bash
git add scripts/remote-smoke.sh README.md docs/remork-product-v1-validation.md
git commit -m "docs: record product v1 validation"
```

## Self-Review Checklist

Before executing this plan:

- [ ] Product V1 daily workflow has tasks for `init`, `sync`, `status`, `apply`, `run`, and `shell`.
- [ ] Advanced workflow has tasks for `pull`, `diff`, `restore`, `log`, and `watch`.
- [ ] Debug and operations workflow has tasks for `doctor`, `debug`, and `daemon`.
- [ ] Shared-token auth is covered in daemon, client, and tests.
- [ ] Local binding is covered and contains no secrets.
- [ ] Large-file prompt, `--force`, and `--quiet` behavior is covered.
- [ ] Operation logs remain per workspace under `<workspace>/.remork/log/operations.jsonl`.
- [ ] `remork run` safe mode blocks dirty local state and supports `--remote-only`.
- [ ] Shell transcript logging is excluded.
- [ ] README is explicitly product-learning-path oriented.
- [ ] Remote validation uses copied binary only and does not install Go or fetch dependencies on remote hosts.

## Execution Handoff

Plan complete. Recommended execution mode is subagent-driven development with one fresh worker per task group:

- Worker A: Tasks 1-3, CLI shell, binding, config, auth.
- Worker B: Tasks 4-8, sync/status/diff/apply/pull.
- Worker C: Tasks 9-11, run/shell/log/watch/doctor/debug.
- Worker D: Tasks 12-14, release packaging, README, validation.

Each worker must edit only the files in its assigned task group, run the task-specific tests, and report changed paths and verification output. Integration should happen after each task, not only at the end.
