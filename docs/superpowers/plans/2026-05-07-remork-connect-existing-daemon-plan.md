# Remork Existing Daemon Connect Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users connect a local workspace to an already running `remorkd` by entering a daemon URL and optional token, while also making `remorkd` easier to configure and install directly on Linux servers.

**Architecture:** Keep the existing HTTP/WebSocket daemon APIs and local workspace binding model. Add shared token-file auth resolution, a `remork connect` spec/command that writes host and workspace config, a server-local `remorkd` config/TUI/start flow, and a Linux npm package exposing `remorkd`. Preserve SSH only for existing daemon install and upgrade commands.

**Tech Stack:** Go 1.22, Cobra CLI, stdlib HTTP/WebSocket clients already in the repo, Charm Bubble Tea TUI primitives already in the repo, `github.com/pelletier/go-toml/v2` for daemon config, Node.js CommonJS npm wrappers, Go unit/e2e tests, Node `node:test`.

---

## File Structure

- Modify `internal/config/config.go`
  - Add `TokenFile` to saved host config without breaking existing `TokenEnv`.
- Modify `internal/config/config_test.go`
  - Cover JSON read/write of host `token_file`.
- Modify `internal/auth/auth.go`
  - Add token-file loading and a shared token-source resolver.
- Modify `internal/auth/auth_test.go`
  - Cover token file trimming, empty file rejection, env priority, and no-token behavior.
- Modify `internal/cli/root.go`
  - Use shared token-source resolution in `httpDaemonProbe` and `clientForHost`.
  - Register `connect` in root help and menu.
- Modify `internal/cli/commands_host.go`
  - Add `--token-file` to manual host config.
- Modify `internal/cli/host_spec.go`
  - Add `TokenFile` to typed host config specs.
- Modify `internal/cli/commands_doctor.go`
  - Report token-file problems in the same readiness path as token-env problems.
- Modify `internal/remoteroot/remoteroot.go`
  - Add workspace path resolution for empty, relative, and absolute connect inputs.
- Modify `internal/remoteroot/remoteroot_test.go`
  - Cover connect workspace path rules.
- Create `internal/cli/connect_spec.go`
  - Own `ConnectSpec`, host-name derivation, token-file path derivation, plan rendering data, and execution.
- Create `internal/cli/connect_spec_test.go`
  - Cover non-interactive connect behavior and root/path resolution through the spec.
- Create `internal/cli/commands_connect.go`
  - Add `remork connect` flags and interactive TUI flow.
- Create `internal/cli/commands_connect_test.go`
  - Cover command registration, non-interactive success, auth failure guidance, and first-sync invocation.
- Modify `internal/cli/setup.go`
  - Route setup menu `Connect to existing daemon` through the connect flow.
- Modify `internal/cli/commands_run.go`, `internal/cli/commands_sync.go`, `internal/cli/commands_status.go`, `internal/cli/commands_shell.go`, `internal/cli/commands_log.go`, `internal/cli/commands_watch.go`, `internal/cli/commands_pull.go`, `internal/cli/commands_apply.go`
  - Add one retry path for interactive token-file auth recovery where each command talks to the daemon.
- Create `internal/cli/auth_recovery.go`
  - Centralize HTTP 401/403 detection, token-file update prompt, and one retry.
- Create `internal/cli/auth_recovery_test.go`
  - Cover token update and non-interactive failure behavior.
- Create `internal/remorkdconfig/config.go`
  - Parse, validate, save, and expand paths in `remorkd.toml`.
- Create `internal/remorkdconfig/config_test.go`
  - Cover TOML parsing, `$HOME` expansion, token generation paths, and unsafe no-token validation.
- Modify `cmd/remorkd/main.go`
  - Add `setup`, `serve --config`, `start`, `stop`, and `status` subcommands while preserving current flags.
- Modify `cmd/remorkd/main_test.go`
  - Cover config-driven serve options and lightweight process command helpers.
- Create `npm/remorkd/bin/remorkd.js`
  - Node wrapper selecting Linux server daemon binary.
- Create `npm/remorkd/test/remorkd-wrapper.test.js`
  - Cover Linux platform mapping and spawn plan behavior.
- Modify `scripts/build-npm-package.sh`
  - Generate both client and server npm package directories.
- Modify `README.md`, `README_ZH.md`, `npm/remork/README.md`
  - Document `remork connect`, token-file auth, and server npm install.
- Create `npm/remorkd/README.md`
  - Document server-side npm install and `remorkd setup`.
- Create `test/e2e/remork_connect_e2e_test.go`
  - Cover daemon start, connect, sync, run, and token rotation recovery.

---

### Task 1: Add Token File Support To Config And Auth

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/auth/auth.go`
- Modify: `internal/auth/auth_test.go`

- [ ] **Step 1: Write failing config and auth tests**

Add this test to `internal/config/config_test.go`:

```go
func TestHostConfigStoresTokenFile(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := Config{
		Hosts: map[string]Host{
			"lab": {
				Name:      "lab",
				URL:       "http://127.0.0.1:17731",
				TokenFile: "/Users/me/.remork/tokens/lab.token",
				NoProxy:  true,
			},
		},
		Workspaces: map[string]Workspace{},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	host := loaded.Hosts["lab"]
	if host.TokenFile != "/Users/me/.remork/tokens/lab.token" {
		t.Fatalf("token_file = %q, want saved path", host.TokenFile)
	}
	if !host.NoProxy {
		t.Fatal("no_proxy should be preserved")
	}
}
```

Add these tests to `internal/auth/auth_test.go`:

```go
func TestTokenFromFileTrimsValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(" file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := TokenFromFile(path)
	if err != nil {
		t.Fatalf("TokenFromFile: %v", err)
	}
	if token != "file-token" {
		t.Fatalf("token = %q, want file-token", token)
	}
}

func TestTokenFromFileRejectsEmptyValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := TokenFromFile(path); err == nil {
		t.Fatal("TokenFromFile error = nil, want empty token error")
	}
}

func TestTokenFromSourceUsesEnvBeforeFile(t *testing.T) {
	t.Setenv("REMORK_TEST_TOKEN", "env-token")
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := TokenFromSource(TokenSource{Env: "REMORK_TEST_TOKEN", File: path})
	if err != nil {
		t.Fatalf("TokenFromSource: %v", err)
	}
	if token != "env-token" {
		t.Fatalf("token = %q, want env-token", token)
	}
}

func TestTokenFromSourceReturnsEmptyWhenNoSource(t *testing.T) {
	token, err := TokenFromSource(TokenSource{})
	if err != nil {
		t.Fatalf("TokenFromSource: %v", err)
	}
	if token != "" {
		t.Fatalf("token = %q, want empty token", token)
	}
}
```

Add imports to `internal/auth/auth_test.go`:

```go
import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/config ./internal/auth -run 'TestHostConfigStoresTokenFile|TestTokenFrom(File|Source)' -count=1
```

Expected: FAIL because `config.Host.TokenFile`, `auth.TokenFromFile`, `auth.TokenSource`, and `auth.TokenFromSource` do not exist.

- [ ] **Step 3: Add host token-file field**

In `internal/config/config.go`, change `Host` to:

```go
type Host struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	TokenEnv  string `json:"token_env,omitempty"`
	TokenFile string `json:"token_file,omitempty"`
	NoProxy   bool   `json:"no_proxy,omitempty"`
}
```

- [ ] **Step 4: Add token-file auth helpers**

In `internal/auth/auth.go`, add:

```go
type TokenSource struct {
	Env  string
	File string
}

func TokenFromFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("token file %q cannot be read: %w", path, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token file %q is empty", path)
	}
	return token, nil
}

func TokenFromSource(src TokenSource) (string, error) {
	if strings.TrimSpace(src.Env) != "" {
		return TokenFromEnv(src.Env)
	}
	if strings.TrimSpace(src.File) != "" {
		return TokenFromFile(src.File)
	}
	return "", nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/config ./internal/auth -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add internal/config/config.go internal/config/config_test.go internal/auth/auth.go internal/auth/auth_test.go
git commit -m "feat: support token file auth config"
```

---

### Task 2: Route Existing CLI Auth Through Token Sources

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/commands_run.go`
- Modify: `internal/cli/commands_sync.go`
- Modify: `internal/cli/commands_status.go`
- Modify: `internal/cli/commands_doctor.go`
- Modify: `internal/cli/commands_host.go`
- Modify: `internal/cli/host_spec.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/cli/commands_workspace_test.go`
- Modify: `internal/cli/commands_doctor_test.go`

- [ ] **Step 1: Add failing tests for token-file host usage**

Add this helper near existing fake probe tests in `internal/cli/root_test.go`:

```go
func writeTestTokenFile(t *testing.T, home, value string) string {
	t.Helper()
	path := filepath.Join(home, ".remork", "tokens", "lab.token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
```

Add this test to `internal/cli/root_test.go`:

```go
func TestDaemonStatusUsesTokenFile(t *testing.T) {
	home := t.TempDir()
	tokenFile := writeTestTokenFile(t, home, "abc123\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer abc123" {
			t.Fatalf("Authorization = %q, want Bearer abc123", got)
		}
		_ = json.NewEncoder(w).Encode(api.StatusResponse{Roots: []string{"/data"}})
	}))
	defer server.Close()

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: server.URL, TokenFile: tokenFile},
		},
		Workspaces: map[string]config.Workspace{},
	}); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	out, err := executeCommand(cmd, "daemon", "status", "lab")
	if err != nil {
		t.Fatalf("daemon status: %v", err)
	}
	mustContain(t, out.String(), "/data")
}
```

Add this test to `internal/cli/commands_workspace_test.go`:

```go
func TestHostAddAcceptsTokenFile(t *testing.T) {
	home := t.TempDir()
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home})
	_, err := executeCommand(cmd, "host", "add", "lab", "--url", "http://127.0.0.1:17731", "--token-file", "/tmp/lab.token", "--no-proxy")
	if err != nil {
		t.Fatalf("host add: %v", err)
	}
	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatal(err)
	}
	host := cfg.Hosts["lab"]
	if host.TokenFile != "/tmp/lab.token" || host.TokenEnv != "" || !host.NoProxy {
		t.Fatalf("host = %#v, want token file and no proxy", host)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli -run 'TestDaemonStatusUsesTokenFile|TestHostAddAcceptsTokenFile' -count=1
```

Expected: FAIL because CLI code still reads only `TokenEnv` and `host add` lacks `--token-file`.

- [ ] **Step 3: Add CLI token-source helper**

In `internal/cli/root.go`, add this helper near `clientForHost`:

```go
func tokenSourceFromHost(host config.Host) auth.TokenSource {
	return auth.TokenSource{Env: host.TokenEnv, File: host.TokenFile}
}
```

Change each `auth.TokenFromEnv(host.TokenEnv)` call in `root.go`, `commands_run.go`, `commands_sync.go`, `commands_status.go`, and `commands_doctor.go` to:

```go
auth.TokenFromSource(tokenSourceFromHost(host))
```

- [ ] **Step 4: Add token-file flag and config persistence**

In `internal/cli/host_spec.go`, change `HostConfigSpec` to:

```go
type HostConfigSpec struct {
	Name      string
	URL       string
	TokenEnv  string
	TokenFile string
	NoProxy   bool
}
```

In `ExecuteHostConfigSpec`, save:

```go
cfg.Hosts[spec.Name] = config.Host{Name: spec.Name, URL: spec.URL, TokenEnv: spec.TokenEnv, TokenFile: spec.TokenFile, NoProxy: spec.NoProxy}
```

In `internal/cli/commands_host.go`, read the flag:

```go
tokenFile, err := cmd.Flags().GetString("token-file")
if err != nil {
	return err
}
cfg.Hosts[name] = config.Host{Name: name, URL: url, TokenEnv: tokenEnv, TokenFile: tokenFile, NoProxy: noProxy}
```

Register the flag:

```go
add.Flags().String("token-file", "", "File containing the daemon token")
```

Update host list rows to show token source:

```go
tokenSource := host.TokenEnv
if tokenSource == "" {
	tokenSource = host.TokenFile
}
rows = append(rows, []string{name, host.URL, tokenSource, flags})
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/cli -run 'TestDaemonStatusUsesTokenFile|TestHostAddAcceptsTokenFile|TestClientSendsClientIDAndBearerToken|TestInitDefaultProbeMissingTokenEnvDoesNotWriteBinding' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run package tests**

Run:

```bash
go test ./internal/cli ./internal/auth ./internal/config -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit Task 2**

```bash
git add internal/cli/root.go internal/cli/commands_run.go internal/cli/commands_sync.go internal/cli/commands_status.go internal/cli/commands_doctor.go internal/cli/commands_host.go internal/cli/host_spec.go internal/cli/root_test.go internal/cli/commands_workspace_test.go
git commit -m "feat: use token files in cli hosts"
```

---

### Task 3: Add Workspace Path Resolution For Connect

**Files:**
- Modify: `internal/remoteroot/remoteroot.go`
- Modify: `internal/remoteroot/remoteroot_test.go`

- [ ] **Step 1: Write failing path-resolution tests**

Add these tests to `internal/remoteroot/remoteroot_test.go`:

```go
func TestResolveWorkspacePathUsesSelectedRootForEmptyInput(t *testing.T) {
	allowed, err := NormalizeMany([]string{"/home/me"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveWorkspacePath(allowed, "/home/me", "")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath: %v", err)
	}
	if got != "/home/me" {
		t.Fatalf("workspace = %q, want /home/me", got)
	}
}

func TestResolveWorkspacePathJoinsRelativeInput(t *testing.T) {
	allowed, err := NormalizeMany([]string{"/home/me"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveWorkspacePath(allowed, "/home/me", "project-a")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath: %v", err)
	}
	if got != "/home/me/project-a" {
		t.Fatalf("workspace = %q, want /home/me/project-a", got)
	}
}

func TestResolveWorkspacePathAllowsAbsoluteInputInsideAnyAllowedRoot(t *testing.T) {
	allowed, err := NormalizeMany([]string{"/home/me", "/scratch/me"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveWorkspacePath(allowed, "/home/me", "/scratch/me/project-a")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath: %v", err)
	}
	if got != "/scratch/me/project-a" {
		t.Fatalf("workspace = %q, want /scratch/me/project-a", got)
	}
}

func TestResolveWorkspacePathRejectsAbsoluteInputOutsideAllowedRoots(t *testing.T) {
	allowed, err := NormalizeMany([]string{"/home/me"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveWorkspacePath(allowed, "/home/me", "/var/tmp/project")
	if err == nil {
		t.Fatal("ResolveWorkspacePath error = nil, want outside-root error")
	}
	if !strings.Contains(err.Error(), "outside advertised allowed roots") {
		t.Fatalf("error = %q, want outside-root guidance", err.Error())
	}
}

func TestResolveWorkspacePathRejectsTildeInput(t *testing.T) {
	allowed, err := NormalizeMany([]string{"/home/me"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveWorkspacePath(allowed, "/home/me", "~/project")
	if err == nil {
		t.Fatal("ResolveWorkspacePath error = nil, want tilde error")
	}
	if !strings.Contains(err.Error(), "use an absolute remote path") {
		t.Fatalf("error = %q, want tilde guidance", err.Error())
	}
}
```

Add `strings` to the test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/remoteroot -run TestResolveWorkspacePath -count=1
```

Expected: FAIL because `ResolveWorkspacePath` does not exist.

- [ ] **Step 3: Implement workspace path resolver**

Add this function to `internal/remoteroot/remoteroot.go`:

```go
func ResolveWorkspacePath(allowed []Root, selectedRoot string, input string) (string, error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "~") {
		return "", fmt.Errorf("workspace path %q is not expanded by remork connect; use an absolute remote path such as /home/me/project", input)
	}
	if input == "" {
		base, err := Normalize(selectedRoot)
		if err != nil {
			return "", err
		}
		ok, err := containsClean(allowed, base.Clean)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("workspace path %q is outside advertised allowed roots", base.Clean)
		}
		return base.Clean, nil
	}
	if isRemoteAbs(input) {
		candidate, err := Normalize(input)
		if err != nil {
			return "", err
		}
		ok, err := containsClean(allowed, candidate.Clean)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("workspace path %q is outside advertised allowed roots", candidate.Clean)
		}
		return candidate.Clean, nil
	}
	base, err := Normalize(selectedRoot)
	if err != nil {
		return "", err
	}
	candidate := cleanRemote(path.Join(base.Clean, input))
	ok, err := containsClean(allowed, candidate)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("workspace path %q is outside advertised allowed roots", candidate)
	}
	return candidate, nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/remoteroot -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 3**

```bash
git add internal/remoteroot/remoteroot.go internal/remoteroot/remoteroot_test.go
git commit -m "feat: resolve connect workspace paths"
```

---

### Task 4: Add ConnectSpec Core Execution

**Files:**
- Create: `internal/cli/connect_spec.go`
- Create: `internal/cli/connect_spec_test.go`

- [ ] **Step 1: Write failing ConnectSpec tests**

Create `internal/cli/connect_spec_test.go`:

```go
package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/ops"
	"remork/internal/workspace"
)

type connectProbe struct {
	roots         []string
	manifestRoot string
}

func (p *connectProbe) Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error) {
	return api.StatusResponse{Roots: p.roots}, nil
}

func (p *connectProbe) Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error) {
	p.manifestRoot = root
	return api.ManifestResponse{Root: root, Path: "."}, nil
}

func (p *connectProbe) Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error) {
	return nil, nil
}

func TestDeriveConnectHostName(t *testing.T) {
	got, err := deriveConnectHostName("http://lab.example.internal:17731")
	if err != nil {
		t.Fatalf("deriveConnectHostName: %v", err)
	}
	if got != "lab-example-internal-17731" {
		t.Fatalf("host = %q, want lab-example-internal-17731", got)
	}
}

func TestExecuteConnectSpecWritesHostTokenAndBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	probe := &connectProbe{roots: []string{"/home/me"}}
	err := ExecuteConnectSpec(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: probe,
	}, ConnectSpec{
		URL:           "http://lab.example.internal:17731",
		HostName:      "lab",
		Token:         "secret-token",
		SelectedRoot:  "/home/me",
		WorkspacePath: "project-a",
		FirstSync:     false,
	})
	if err != nil {
		t.Fatalf("ExecuteConnectSpec: %v", err)
	}

	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatal(err)
	}
	host := cfg.Hosts["lab"]
	if host.URL != "http://lab.example.internal:17731" {
		t.Fatalf("host URL = %q", host.URL)
	}
	if host.TokenFile == "" {
		t.Fatal("host token file was not saved")
	}
	data, err := os.ReadFile(host.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret-token\n" {
		t.Fatalf("token file = %q, want secret-token newline", data)
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if binding.Host != "lab" || binding.RemoteRoot != "/home/me/project-a" {
		t.Fatalf("binding = %#v", binding)
	}
	if probe.manifestRoot != "/home/me/project-a" {
		t.Fatalf("manifest root = %q, want /home/me/project-a", probe.manifestRoot)
	}
}

func TestExecuteConnectSpecRejectsManifestOutsideRootsWithoutBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	probe := &connectProbe{roots: []string{"/home/me"}}
	err := ExecuteConnectSpec(Options{
		Version:     "test",
		HomeDir:     home,
		WorkingDir:  local,
		DaemonProbe: probe,
	}, ConnectSpec{
		URL:           "http://lab.example.internal:17731",
		HostName:      "lab",
		SelectedRoot:  "/home/me",
		WorkspacePath: "/var/tmp/project",
	})
	if err == nil {
		t.Fatal("ExecuteConnectSpec error = nil, want outside-root error")
	}
	if _, _, resolveErr := workspace.ResolveFrom(local); resolveErr == nil {
		t.Fatal("workspace binding should not be written on failed connect")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli -run 'Test(DeriveConnectHostName|ExecuteConnectSpec)' -count=1
```

Expected: FAIL because `ConnectSpec`, `deriveConnectHostName`, and `ExecuteConnectSpec` do not exist.

- [ ] **Step 3: Implement ConnectSpec**

Create `internal/cli/connect_spec.go`:

```go
package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"remork/internal/config"
	"remork/internal/remoteroot"
)

type ConnectSpec struct {
	URL           string
	HostName      string
	Token         string
	TokenEnv      string
	TokenFile     string
	NoProxy       bool
	SelectedRoot  string
	WorkspacePath string
	LocalRoot      string
	FirstSync     bool
}

var nonHostNameChars = regexp.MustCompile(`[^A-Za-z0-9]+`)

func deriveConnectHostName(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("daemon URL must include a host")
	}
	host := parsed.Hostname()
	if port := parsed.Port(); port != "" {
		host += "-" + port
	}
	name := strings.Trim(nonHostNameChars.ReplaceAllString(host, "-"), "-")
	if name == "" {
		return "", fmt.Errorf("could not derive host name from %q", rawURL)
	}
	return strings.ToLower(name), nil
}

func defaultConnectTokenFile(homeDir, hostName string) string {
	return filepath.Join(homeDir, ".remork", "tokens", hostName+".token")
}

func writeConnectTokenFile(path, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600)
}

func ExecuteConnectSpec(opts Options, spec ConnectSpec) error {
	if err := validateDaemonURL(spec.URL); err != nil {
		return err
	}
	if spec.HostName == "" {
		name, err := deriveConnectHostName(spec.URL)
		if err != nil {
			return err
		}
		spec.HostName = name
	}
	if spec.LocalRoot == "" {
		spec.LocalRoot = opts.WorkingDir
	}
	if spec.LocalRoot == "" {
		return fmt.Errorf("local root is required")
	}
	if spec.Token != "" && spec.TokenEnv == "" && spec.TokenFile == "" {
		spec.TokenFile = defaultConnectTokenFile(opts.HomeDir, spec.HostName)
	}
	if err := writeConnectTokenFile(spec.TokenFile, spec.Token); err != nil {
		return err
	}

	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	host := config.Host{Name: spec.HostName, URL: spec.URL, TokenEnv: spec.TokenEnv, TokenFile: spec.TokenFile, NoProxy: spec.NoProxy}
	status, err := opts.DaemonProbe.Status(context.Background(), host, cfg.ClientID)
	if err != nil {
		return err
	}
	if len(status.Roots) == 0 {
		return fmt.Errorf("daemon did not advertise any allowed roots")
	}
	allowed, err := remoteroot.NormalizeMany(status.Roots)
	if err != nil {
		return err
	}
	selected := spec.SelectedRoot
	if selected == "" {
		selected = status.Roots[0]
	}
	remoteRoot, err := remoteroot.ResolveWorkspacePath(allowed, selected, spec.WorkspacePath)
	if err != nil {
		return err
	}
	if _, err := opts.DaemonProbe.Manifest(context.Background(), host, cfg, remoteRoot); err != nil {
		return err
	}
	cfg.Hosts[spec.HostName] = host
	if err := store.Save(cfg); err != nil {
		return err
	}
	return ExecuteWorkspaceBindSpec(opts, WorkspaceBindSpec{
		HostName:   spec.HostName,
		RemoteRoot: remoteRoot,
		LocalRoot:  spec.LocalRoot,
	})
}
```

- [ ] **Step 4: Fix imports**

`internal/cli/connect_spec.go` must import `context` because `ExecuteConnectSpec` probes the daemon with `context.Background()`.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/cli -run 'Test(DeriveConnectHostName|ExecuteConnectSpec)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 4**

```bash
git add internal/cli/connect_spec.go internal/cli/connect_spec_test.go
git commit -m "feat: add connect spec execution"
```

---

### Task 5: Add Non-Interactive `remork connect`

**Files:**
- Create: `internal/cli/commands_connect.go`
- Create: `internal/cli/commands_connect_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write failing command tests**

Create `internal/cli/commands_connect_test.go`:

```go
package cli

import (
	"path/filepath"
	"testing"

	"remork/internal/config"
	"remork/internal/workspace"
)

func TestConnectCommandIsRegistered(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	found, _, err := cmd.Find([]string{"connect"})
	if err != nil || found == nil {
		t.Fatalf("connect command not registered: %v", err)
	}
}

func TestConnectCommandNonInteractiveBindsWorkspace(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	probe := &connectProbe{roots: []string{"/home/me"}}
	cmd := NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local, DaemonProbe: probe})
	out, err := executeCommand(cmd, "connect", "--url", "http://lab.example.internal:17731", "--host", "lab", "--workspace-path", "project-a", "--token", "secret", "--first-sync=false", "--non-interactive")
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, out.String())
	}
	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hosts["lab"].TokenFile == "" {
		t.Fatal("connect should save a token file")
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatal(err)
	}
	if binding.RemoteRoot != "/home/me/project-a" {
		t.Fatalf("remote root = %q", binding.RemoteRoot)
	}
	mustContain(t, out.String(), "connected")
}

func TestConnectCommandNonInteractiveRequiresURL(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir(), DaemonProbe: &connectProbe{roots: []string{"/data"}}})
	_, err := executeCommand(cmd, "connect", "--non-interactive")
	if err == nil {
		t.Fatal("connect without URL should fail")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli -run 'TestConnectCommand' -count=1
```

Expected: FAIL because `connect` is not registered.

- [ ] **Step 3: Implement command registration**

In `internal/cli/root.go`, add `connect` to help under Setup:

```text
  connect     Connect this directory to an existing remorkd
```

Register the command after `addSetupCommand(root, opts)`:

```go
addConnectCommand(root, opts)
```

Add a root menu item for unbound directories:

```go
{Group: "Setup", Name: "connect", Description: "Connect this directory to an existing remorkd", Args: []string{"connect"}, HelpOnly: true},
```

- [ ] **Step 4: Implement non-interactive command**

Create `internal/cli/commands_connect.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func addConnectCommand(root *cobra.Command, opts Options) {
	var spec ConnectSpec
	var firstSync bool
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect this directory to an existing remorkd",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec.FirstSync = firstSync
			if spec.URL == "" {
				mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
				if mode.Wizard {
					return runConnectTUI(cmd, opts, spec)
				}
				return codedCommandError{code: 2, err: fmt.Errorf("--url is required"), fix: "pass remork connect --url http://HOST:PORT"}
			}
			if err := ExecuteConnectSpec(opts, spec); err != nil {
				return err
			}
			r := plainRenderer(cmd, false)
			r.Section("Connected")
			r.KeyValue("host", firstNonEmpty(spec.HostName, "derived from URL"))
			r.Success("connected")
			if firstSync {
				cmd.Root().SetArgs([]string{"sync"})
				return cmd.Root().ExecuteContext(cmd.Context())
			}
			r.Next([]string{"remork sync"})
			return nil
		},
	}
	cmd.Flags().StringVar(&spec.URL, "url", "", "Daemon URL")
	cmd.Flags().StringVar(&spec.HostName, "host", "", "Saved host name")
	cmd.Flags().StringVar(&spec.Token, "token", "", "Daemon token to save locally")
	cmd.Flags().StringVar(&spec.TokenEnv, "token-env", "", "Environment variable containing the daemon token")
	cmd.Flags().StringVar(&spec.TokenFile, "token-file", "", "Local token file to read or write")
	cmd.Flags().BoolVar(&spec.NoProxy, "no-proxy", false, "Bypass proxies for this daemon")
	cmd.Flags().StringVar(&spec.SelectedRoot, "root", "", "Advertised allowed root to use as the base for relative workspace paths")
	cmd.Flags().StringVar(&spec.WorkspacePath, "workspace-path", "", "Workspace path, either relative to --root or absolute inside an advertised root")
	cmd.Flags().BoolVar(&firstSync, "first-sync", true, "Run remork sync after connecting")
	root.AddCommand(cmd)
}
```

Add this stub below it so Task 6 can fill in the TUI:

```go
func runConnectTUI(cmd *cobra.Command, opts Options, initial ConnectSpec) error {
	return codedCommandError{code: 2, err: fmt.Errorf("interactive connect is not implemented yet"), fix: "pass --url and --workspace-path, or use remork host add and remork init"}
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/cli -run 'TestConnectCommand' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 5**

```bash
git add internal/cli/root.go internal/cli/commands_connect.go internal/cli/commands_connect_test.go
git commit -m "feat: add remork connect command"
```

---

### Task 6: Add Interactive Connect TUI And Setup Menu Route

**Files:**
- Modify: `internal/cli/commands_connect.go`
- Modify: `internal/cli/setup.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/cli/setup_test.go`

- [ ] **Step 1: Add setup menu test**

Add this test to `internal/cli/root_test.go`:

```go
func TestSetupMenuIncludesExistingDaemonConnect(t *testing.T) {
	items := setupScopeItems(false)
	found := false
	for _, item := range items {
		if item.Name == "Connect to existing daemon" && len(item.Args) == 1 && item.Args[0] == "connect-existing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("setup menu items = %#v, want Connect to existing daemon", items)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestSetupMenuIncludesExistingDaemonConnect -count=1
```

Expected: FAIL because the setup menu only has `Connect this project`.

- [ ] **Step 3: Rename setup menu item and route it**

In `internal/cli/setup.go`, change the unbound setup item:

```go
{Name: "Connect to existing daemon", Description: "Enter a daemon URL, bind this directory, then offer first sync", Args: []string{"connect-existing"}},
```

In `runSetupScopeMenu`, add:

```go
case "connect-existing":
	return runConnectTUI(cmd, opts, ConnectSpec{FirstSync: true})
```

Keep the existing `case "connect"` if tests still depend on it:

```go
case "connect":
	return runSetupConnectProject(cmd, opts)
```

- [ ] **Step 4: Implement TUI fields**

Replace the `runConnectTUI` stub in `internal/cli/commands_connect.go` with:

```go
func runConnectTUI(cmd *cobra.Command, opts Options, initial ConnectSpec) error {
	values, err := runTUIForm(cmd, "Connect to existing daemon", []tui.Field{
		{Section: "Daemon", Key: "url", Label: "Daemon URL", Placeholder: "http://server:17731", Initial: initial.URL, Help: "HTTP URL for an already running remorkd."},
		{Section: "Daemon", Key: "host", Label: "Host name", Placeholder: "auto", Initial: initial.HostName, Help: "Saved local name. Leave empty to derive one from the URL."},
		{Section: "Auth", Key: "token", Label: "Token", Initial: initial.Token, Help: "Optional. Leave empty for unauthenticated private-network daemons."},
		{Section: "Auth", Key: "token_file", Label: "Token file", Initial: initial.TokenFile, Help: "Optional. Defaults to ~/.remork/tokens/<host>.token when a token is entered."},
		{Section: "Workspace", Key: "root", Label: "Allowed root", Initial: initial.SelectedRoot, Help: "Advertised daemon root used as the base for relative workspace paths."},
		{Section: "Workspace", Key: "workspace_path", Label: "Workspace path", Initial: initial.WorkspacePath, Help: "Empty uses the allowed root; relative paths join under it; absolute paths must be inside an advertised root."},
		{Section: "Network", Key: "no_proxy", Label: "Bypass proxy y/N", Placeholder: "no", Initial: yesNo(initial.NoProxy), Help: "Use yes for VPN or private IPs that should bypass local proxy variables."},
		{Section: "First run", Key: "first_sync", Label: "Run first sync y/N", Placeholder: "yes", Initial: yesNo(initial.FirstSync), Help: "Download current remote files after binding."},
	})
	if err != nil {
		return err
	}
	noProxy, err := parseDaemonDeployBool(values["no_proxy"], "no proxy")
	if err != nil {
		return err
	}
	firstSync, err := parseDaemonDeployBool(values["first_sync"], "first sync")
	if err != nil {
		return err
	}
	spec := ConnectSpec{
		URL:           strings.TrimSpace(values["url"]),
		HostName:      strings.TrimSpace(values["host"]),
		Token:         strings.TrimSpace(values["token"]),
		TokenFile:     strings.TrimSpace(values["token_file"]),
		NoProxy:       noProxy,
		SelectedRoot:  strings.TrimSpace(values["root"]),
		WorkspacePath: strings.TrimSpace(values["workspace_path"]),
		FirstSync:     firstSync,
	}
	if err := ExecuteConnectSpec(opts, spec); err != nil {
		return err
	}
	plainRenderer(cmd, false).Success("connected")
	if firstSync {
		cmd.Root().SetArgs([]string{"sync"})
		return cmd.Root().ExecuteContext(cmd.Context())
	}
	plainRenderer(cmd, false).Next([]string{"remork sync"})
	return nil
}
```

Add imports:

```go
import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/tui"
)
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/cli -run 'TestSetupMenuIncludesExistingDaemonConnect|TestConnectCommand' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 6**

```bash
git add internal/cli/commands_connect.go internal/cli/setup.go internal/cli/root_test.go
git commit -m "feat: add interactive existing daemon connect"
```

---

### Task 7: Add Token Recovery Helper For Interactive Commands

**Files:**
- Create: `internal/cli/auth_recovery.go`
- Create: `internal/cli/auth_recovery_test.go`
- Modify: `internal/cli/commands_run.go`
- Modify: `internal/cli/commands_sync.go`
- Modify: `internal/cli/commands_status.go`
- Modify: `internal/cli/commands_shell.go`
- Modify: `internal/cli/commands_log.go`
- Modify: `internal/cli/commands_watch.go`
- Modify: `internal/cli/commands_pull.go`
- Modify: `internal/cli/commands_apply.go`

- [ ] **Step 1: Write failing auth recovery tests**

Create `internal/cli/auth_recovery_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/cli -run 'Test(IsAuthHTTPError|UpdateHostTokenFile|AuthRecovery)' -count=1
```

Expected: FAIL because helper functions do not exist.

- [ ] **Step 3: Add helper functions**

Create `internal/cli/auth_recovery.go`:

```go
package cli

import (
	"errors"
	"strings"

	"remork/internal/client"
	"remork/internal/config"
)

func isAuthHTTPError(err error) bool {
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return httpErr.StatusCode == 401 || httpErr.StatusCode == 403
}

func updateHostTokenFile(homeDir string, host config.Host, token string) (config.Host, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return host, errors.New("token cannot be empty")
	}
	if host.TokenFile == "" {
		host.TokenFile = defaultConnectTokenFile(homeDir, host.Name)
	}
	if err := writeConnectTokenFile(host.TokenFile, token); err != nil {
		return host, err
	}
	host.TokenEnv = ""
	return host, nil
}
```

- [ ] **Step 4: Add command retry integration point**

In `internal/cli/commands_run.go`, add fields to `runContext`:

```go
cfg  config.Config
host config.Host
```

Return them from `newRunContext`:

```go
return runContext{binding: binding, client: c, runner: runner, baseURL: host.URL, clientID: cfg.ClientID, token: token, noProxy: host.NoProxy, cfg: cfg, host: host}, nil
```

Add this method in `commands_run.go`:

```go
func (ctx runContext) withUpdatedHost(host config.Host) runContext {
	ctx.host = host
	ctx.cfg.Hosts[host.Name] = host
	token, _ := auth.TokenFromSource(tokenSourceFromHost(host))
	ctx.token = token
	ctx.client = clientForHost(host, ctx.cfg, token)
	ctx.baseURL = host.URL
	ctx.noProxy = host.NoProxy
	return ctx
}
```

Add imports for `remork/internal/auth` and `remork/internal/config` if needed.

- [ ] **Step 5: Add retry wrapper**

In `auth_recovery.go`, add:

```go
func retryAfterTokenFileUpdate(cmd *cobra.Command, opts Options, runCtx runContext, err error, retry func(runContext) error) error {
	if !isAuthHTTPError(err) {
		return err
	}
	if boolFlag(cmd, "non-interactive") || !commandHasPromptTTY(cmd) {
		return codedCommandError{code: 2, err: err, fix: "run remork connect --url " + runCtx.host.URL + " to update the saved token"}
	}
	if runCtx.host.TokenEnv != "" {
		return codedCommandError{code: 2, err: err, fix: "update " + runCtx.host.TokenEnv + " with the new daemon token"}
	}
	values, promptErr := runTUIForm(cmd, "Update daemon token", []tui.Field{
		{Section: "Auth", Key: "token", Label: "Token", Help: "Paste the current daemon token."},
	})
	if promptErr != nil {
		return promptErr
	}
	host, updateErr := updateHostTokenFile(opts.HomeDir, runCtx.host, values["token"])
	if updateErr != nil {
		return updateErr
	}
	store, storeErr := configStore(opts)
	if storeErr != nil {
		return storeErr
	}
	cfg, loadErr := store.LoadOrDefault()
	if loadErr != nil {
		return loadErr
	}
	cfg.Hosts[host.Name] = host
	if saveErr := store.Save(cfg); saveErr != nil {
		return saveErr
	}
	return retry(runCtx.withUpdatedHost(host))
}
```

Add imports in `auth_recovery.go`:

```go
import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/client"
	"remork/internal/config"
	"remork/internal/tui"
)
```

- [ ] **Step 6: Wrap daily command remote operations**

For each daily command that calls remote APIs after `newRunContext(opts)`, use this pattern:

```go
err := doCommand(runCtx)
if err != nil {
	return retryAfterTokenFileUpdate(cmd, opts, runCtx, err, doCommand)
}
return nil
```

For `run`, make the body that calls `runCtx.client.ExecContext` into a local `doRun` function:

```go
doRun := func(active runContext) error {
	result, err := active.client.ExecContext(ctx, active.binding.RemoteRoot, active.binding.RemoteRoot, command, timeout.Milliseconds())
	if err != nil {
		return err
	}
	renderRunResult(cmd, result)
	return nil
}
if err := doRun(runCtx); err != nil {
	return retryAfterTokenFileUpdate(cmd, opts, runCtx, err, doRun)
}
return nil
```

Apply the same wrapper to `sync`, `status`, `shell`, `log`, `watch`, `pull`, and `apply` around the first daemon call that can return `client.HTTPError`.

- [ ] **Step 7: Run helper tests**

Run:

```bash
go test ./internal/cli -run 'Test(IsAuthHTTPError|UpdateHostTokenFile|AuthRecovery)' -count=1
```

Expected: PASS.

- [ ] **Step 8: Run CLI tests**

Run:

```bash
go test ./internal/cli -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit Task 7**

```bash
git add internal/cli/auth_recovery.go internal/cli/auth_recovery_test.go internal/cli/commands_run.go internal/cli/commands_sync.go internal/cli/commands_status.go internal/cli/commands_shell.go internal/cli/commands_log.go internal/cli/commands_watch.go internal/cli/commands_pull.go internal/cli/commands_apply.go
git commit -m "feat: recover expired daemon tokens"
```

---

### Task 8: Add `remorkd` Config File Model

**Files:**
- Create: `internal/remorkdconfig/config.go`
- Create: `internal/remorkdconfig/config_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add TOML dependency**

Run:

```bash
go get github.com/pelletier/go-toml/v2@v2.2.3
```

Expected: `go.mod` and `go.sum` include `github.com/pelletier/go-toml/v2`.

- [ ] **Step 2: Write failing config tests**

Create `internal/remorkdconfig/config_test.go`:

```go
package remorkdconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExpandsHomeAndParsesConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "remorkd.toml")
	data := []byte(`
listen_addr = "0.0.0.0:17731"
allowed_roots = ["/home/me", "/scratch/me"]
large_file_threshold = "128MB"
token_file = "$HOME/.remork/remork.token"
pid_file = "$HOME/.remork/run/remorkd.pid"
log_file = "$HOME/.remork/log/remorkd.log"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:17731" {
		t.Fatalf("listen = %q", cfg.ListenAddr)
	}
	if cfg.TokenFile != filepath.Join(home, ".remork", "remork.token") {
		t.Fatalf("token file = %q", cfg.TokenFile)
	}
	if len(cfg.AllowedRoots) != 2 || cfg.AllowedRoots[1] != "/scratch/me" {
		t.Fatalf("roots = %#v", cfg.AllowedRoots)
	}
}

func TestValidateRejectsNoRoots(t *testing.T) {
	err := Validate(Config{ListenAddr: "127.0.0.1:17731"})
	if err == nil {
		t.Fatal("Validate error = nil, want root error")
	}
}

func TestDefaultPathUsesHome(t *testing.T) {
	home := "/home/me"
	if got := DefaultPath(home); got != "/home/me/.remork/remorkd.toml" {
		t.Fatalf("DefaultPath = %q", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
go test ./internal/remorkdconfig -count=1
```

Expected: FAIL because package does not exist.

- [ ] **Step 4: Implement config model**

Create `internal/remorkdconfig/config.go`:

```go
package remorkdconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	ListenAddr         string   `toml:"listen_addr"`
	AllowedRoots       []string `toml:"allowed_roots"`
	LargeFileThreshold string   `toml:"large_file_threshold"`
	TokenFile          string   `toml:"token_file,omitempty"`
	PIDFile            string   `toml:"pid_file"`
	LogFile            string   `toml:"log_file"`
}

func DefaultPath(home string) string {
	return filepath.Join(home, ".remork", "remorkd.toml")
}

func Default(home string) Config {
	return Config{
		ListenAddr:         "0.0.0.0:17731",
		LargeFileThreshold: "128MB",
		TokenFile:          filepath.Join(home, ".remork", "remork.token"),
		PIDFile:            filepath.Join(home, ".remork", "run", "remorkd.pid"),
		LogFile:            filepath.Join(home, ".remork", "log", "remorkd.log"),
	}
}

func Load(path string, home string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.expand(home)
	return cfg, Validate(cfg)
}

func Save(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Validate(cfg Config) error {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return fmt.Errorf("listen_addr is required")
	}
	if len(cfg.AllowedRoots) == 0 {
		return fmt.Errorf("allowed_roots must contain at least one root")
	}
	for _, root := range cfg.AllowedRoots {
		if !strings.HasPrefix(strings.TrimSpace(root), "/") {
			return fmt.Errorf("allowed root %q must be absolute", root)
		}
	}
	return nil
}

func (cfg *Config) expand(home string) {
	cfg.TokenFile = expandHome(cfg.TokenFile, home)
	cfg.PIDFile = expandHome(cfg.PIDFile, home)
	cfg.LogFile = expandHome(cfg.LogFile, home)
}

func expandHome(path string, home string) string {
	if strings.HasPrefix(path, "$HOME/") {
		return filepath.Join(home, strings.TrimPrefix(path, "$HOME/"))
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/remorkdconfig -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 8**

```bash
git add go.mod go.sum internal/remorkdconfig/config.go internal/remorkdconfig/config_test.go
git commit -m "feat: add remorkd config model"
```

---

### Task 9: Add `remorkd serve --config`

**Files:**
- Modify: `cmd/remorkd/main.go`
- Modify: `cmd/remorkd/main_test.go`

- [ ] **Step 1: Write failing serve option tests**

Add this test to `cmd/remorkd/main_test.go`:

```go
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
```

Add import:

```go
import "remork/internal/remorkdconfig"
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./cmd/remorkd -run TestServerConfigBuildsDaemonOptions -count=1
```

Expected: FAIL because `serverOptionsFromConfig` does not exist.

- [ ] **Step 3: Refactor daemon startup options**

In `cmd/remorkd/main.go`, add:

```go
type serverOptions struct {
	Addr    string
	Roots   []string
	Token   string
	Version string
}

func serverOptionsFromConfig(cfg remorkdconfig.Config, version string) (serverOptions, error) {
	token, err := resolveToken("", cfg.TokenFile)
	if err != nil {
		return serverOptions{}, err
	}
	return serverOptions{Addr: cfg.ListenAddr, Roots: cfg.AllowedRoots, Token: token, Version: version}, nil
}

func runServer(opts serverOptions) error {
	if len(opts.Roots) == 0 {
		return fmt.Errorf("--root is required")
	}
	if insecureNoTokenNonLoopbackListenAddr(opts.Addr, opts.Token != "") {
		log.Printf("WARNING: remorkd is listening on a non-loopback or wildcard address without authentication; clients that can reach it can use apply/file access and writes, remote command execution, and shell endpoints. Use --token-file and configure clients with remork connect.")
	}
	srv := daemon.NewServer(daemon.Config{Version: opts.Version, Roots: opts.Roots, LargeThreshold: 128 << 20, Token: opts.Token})
	httpServer := &http.Server{
		Addr:              opts.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: limits.DaemonReadHeaderTimeout,
		IdleTimeout:       limits.DaemonIdleTimeout,
	}
	return httpServer.ListenAndServe()
}
```

Add `remork/internal/remorkdconfig` to imports.

- [ ] **Step 4: Add `serve --config` argument handling**

At the start of `main()`, before current flag parsing, add:

```go
if len(os.Args) > 1 {
	switch os.Args[1] {
	case "serve":
		if err := runServeCommand(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
}
```

Add:

```go
func runServeCommand(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := fs.String("config", remorkdconfig.DefaultPath(home), "remorkd config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := remorkdconfig.Load(*configPath, home)
	if err != nil {
		return err
	}
	opts, err := serverOptionsFromConfig(cfg, version)
	if err != nil {
		return err
	}
	return runServer(opts)
}
```

Then replace the old inline server construction in `main()` with:

```go
log.Fatal(runServer(serverOptions{Addr: *addr, Roots: []string(roots), Token: resolvedToken, Version: version}))
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./cmd/remorkd -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 9**

```bash
git add cmd/remorkd/main.go cmd/remorkd/main_test.go
git commit -m "feat: serve remorkd from config"
```

---

### Task 10: Add `remorkd setup`

**Files:**
- Modify: `cmd/remorkd/main.go`
- Modify: `cmd/remorkd/main_test.go`

- [ ] **Step 1: Write failing setup helper tests**

Add this test to `cmd/remorkd/main_test.go`:

```go
func TestSetupValuesBuildConfigAndToken(t *testing.T) {
	home := t.TempDir()
	cfg, token, err := configFromSetupValues(home, map[string]string{
		"listen_addr":          "0.0.0.0:17731",
		"allowed_roots":        "/home/me, /scratch/me",
		"token_mode":           "generate",
		"token_file":           "$HOME/.remork/remork.token",
		"large_file_threshold": "128MB",
		"pid_file":             "$HOME/.remork/run/remorkd.pid",
		"log_file":             "$HOME/.remork/log/remorkd.log",
	})
	if err != nil {
		t.Fatalf("configFromSetupValues: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:17731" {
		t.Fatalf("listen = %q", cfg.ListenAddr)
	}
	if len(cfg.AllowedRoots) != 2 || cfg.AllowedRoots[1] != "/scratch/me" {
		t.Fatalf("roots = %#v", cfg.AllowedRoots)
	}
	if cfg.TokenFile != filepath.Join(home, ".remork", "remork.token") {
		t.Fatalf("token file = %q", cfg.TokenFile)
	}
	if token == "" {
		t.Fatal("generated token should not be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./cmd/remorkd -run TestSetupValuesBuildConfigAndToken -count=1
```

Expected: FAIL because `configFromSetupValues` does not exist.

- [ ] **Step 3: Add setup value parser**

In `cmd/remorkd/main.go`, add:

```go
func configFromSetupValues(home string, values map[string]string) (remorkdconfig.Config, string, error) {
	roots := splitComma(values["allowed_roots"])
	cfg := remorkdconfig.Config{
		ListenAddr:         firstValue(values["listen_addr"], "0.0.0.0:17731"),
		AllowedRoots:       roots,
		LargeFileThreshold: firstValue(values["large_file_threshold"], "128MB"),
		TokenFile:          remorkdconfig.ExpandHome(firstValue(values["token_file"], "$HOME/.remork/remork.token"), home),
		PIDFile:            remorkdconfig.ExpandHome(firstValue(values["pid_file"], "$HOME/.remork/run/remorkd.pid"), home),
		LogFile:            remorkdconfig.ExpandHome(firstValue(values["log_file"], "$HOME/.remork/log/remorkd.log"), home),
	}
	token := ""
	switch strings.TrimSpace(values["token_mode"]) {
	case "", "generate":
		token = randomToken()
	case "paste", "update":
		token = strings.TrimSpace(values["token"])
	case "none":
		cfg.TokenFile = ""
	default:
		return remorkdconfig.Config{}, "", fmt.Errorf("unknown token mode %q", values["token_mode"])
	}
	return cfg, token, remorkdconfig.Validate(cfg)
}

func splitComma(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
```

To avoid exporting a test-only function from `internal/remorkdconfig`, instead export the existing `expandHome` helper as:

```go
func ExpandHome(path string, home string) string {
	return expandHome(path, home)
}
```

Use `remorkdconfig.ExpandHome` in `cmd/remorkd/main.go`.

Add imports:

```go
import (
	"crypto/rand"
	"encoding/hex"
)
```

- [ ] **Step 4: Add setup subcommand**

In the main subcommand switch, add:

```go
case "setup":
	if err := runSetupCommand(os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	return
```

Add:

```go
func runSetupCommand(args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := remorkdconfig.DefaultPath(home)
	values, err := tui.RunForm(tui.NewFormModel("remorkd setup", []tui.Field{
		{Section: "Network", Key: "listen_addr", Label: "Listen address", Initial: "0.0.0.0:17731"},
		{Section: "Workspace", Key: "allowed_roots", Label: "Allowed roots", Placeholder: "/home/me, /scratch/me"},
		{Section: "Auth", Key: "token_mode", Label: "Token mode", Initial: "generate", Help: "generate, paste, update, or none"},
		{Section: "Auth", Key: "token", Label: "Token", Help: "Used only for paste or update mode."},
		{Section: "Auth", Key: "token_file", Label: "Token file", Initial: "$HOME/.remork/remork.token"},
		{Section: "Files", Key: "large_file_threshold", Label: "Large file threshold", Initial: "128MB"},
		{Section: "Files", Key: "pid_file", Label: "PID file", Initial: "$HOME/.remork/run/remorkd.pid"},
		{Section: "Files", Key: "log_file", Label: "Log file", Initial: "$HOME/.remork/log/remorkd.log"},
	}))
	if err != nil {
		return err
	}
	cfg, token, err := configFromSetupValues(home, values)
	if err != nil {
		return err
	}
	if token != "" && cfg.TokenFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.TokenFile), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(cfg.TokenFile, []byte(token+"\n"), 0o600); err != nil {
			return err
		}
	}
	if err := remorkdconfig.Save(configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Config written: %s\n", configPath)
	fmt.Printf("Start daemon: remorkd start --config %s\n", configPath)
	fmt.Printf("Client connect: remork connect --url http://HOST:%s\n", daemonPort(cfg.ListenAddr))
	return nil
}
```

Add a local `daemonPort` helper or reuse an existing one only if it is in this package.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./cmd/remorkd ./internal/remorkdconfig -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 10**

```bash
git add cmd/remorkd/main.go cmd/remorkd/main_test.go internal/remorkdconfig/config.go internal/remorkdconfig/config_test.go
git commit -m "feat: add remorkd setup tui"
```

---

### Task 11: Add Lightweight `remorkd start`, `stop`, And `status`

**Files:**
- Modify: `cmd/remorkd/main.go`
- Modify: `cmd/remorkd/main_test.go`

- [ ] **Step 1: Write failing process helper tests**

Add this test to `cmd/remorkd/main_test.go`:

```go
func TestStopCommandReadsPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remorkd.pid")
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, err := readPIDFile(path)
	if err != nil {
		t.Fatalf("readPIDFile: %v", err)
	}
	if pid != 999999 {
		t.Fatalf("pid = %d, want 999999", pid)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./cmd/remorkd -run TestStopCommandReadsPIDFile -count=1
```

Expected: FAIL because `readPIDFile` does not exist.

- [ ] **Step 3: Add PID helper**

In `cmd/remorkd/main.go`, add:

```go
func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}
```

Add `strconv` to imports.

- [ ] **Step 4: Add start/stop/status subcommands**

In the main subcommand switch, add:

```go
case "start":
	if err := runStartCommand(os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	return
case "stop":
	if err := runStopCommand(os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	return
case "status":
	if err := runStatusCommand(os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	return
```

Add command functions:

```go
func loadConfigForProcessCommand(args []string) (remorkdconfig.Config, string, error) {
	fs := flag.NewFlagSet("process", flag.ContinueOnError)
	home, err := os.UserHomeDir()
	if err != nil {
		return remorkdconfig.Config{}, "", err
	}
	configPath := fs.String("config", remorkdconfig.DefaultPath(home), "remorkd config file")
	if err := fs.Parse(args); err != nil {
		return remorkdconfig.Config{}, "", err
	}
	cfg, err := remorkdconfig.Load(*configPath, home)
	return cfg, *configPath, err
}

func runStartCommand(args []string) error {
	cfg, configPath, err := loadConfigForProcessCommand(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.PIDFile), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(os.Args[0], "serve", "--config", configPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := os.WriteFile(cfg.PIDFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("remorkd started pid %d\n", cmd.Process.Pid)
	return nil
}

func runStopCommand(args []string) error {
	cfg, _, err := loadConfigForProcessCommand(args)
	if err != nil {
		return err
	}
	pid, err := readPIDFile(cfg.PIDFile)
	if err != nil {
		return err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(os.Interrupt); err != nil {
		return err
	}
	_ = os.Remove(cfg.PIDFile)
	fmt.Printf("remorkd stopped pid %d\n", pid)
	return nil
}

func runStatusCommand(args []string) error {
	cfg, _, err := loadConfigForProcessCommand(args)
	if err != nil {
		return err
	}
	pid, err := readPIDFile(cfg.PIDFile)
	if err != nil {
		return err
	}
	fmt.Printf("remorkd pid %d\n", pid)
	fmt.Printf("listen %s\n", cfg.ListenAddr)
	return nil
}
```

Add `os/exec` to imports.

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./cmd/remorkd -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 11**

```bash
git add cmd/remorkd/main.go cmd/remorkd/main_test.go
git commit -m "feat: manage remorkd from config"
```

---

### Task 12: Add Server npm Package

**Files:**
- Create: `npm/remorkd/bin/remorkd.js`
- Create: `npm/remorkd/test/remorkd-wrapper.test.js`
- Create: `npm/remorkd/README.md`
- Modify: `scripts/build-npm-package.sh`

- [ ] **Step 1: Write server wrapper tests**

Create `npm/remorkd/test/remorkd-wrapper.test.js`:

```js
const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");

const wrapper = require("../bin/remorkd.js");

test("selects supported Linux daemon binaries", () => {
  assert.equal(wrapper.daemonBinaryName({ platform: "linux", arch: "arm64" }), "remorkd-linux-arm64");
  assert.equal(wrapper.daemonBinaryName({ platform: "linux", arch: "x64" }), "remorkd-linux-amd64");
});

test("rejects unsupported server platform", () => {
  assert.throws(
    () => wrapper.daemonBinaryName({ platform: "darwin", arch: "arm64" }),
    /unsupported Remork daemon platform/,
  );
});

test("builds spawn plan with args", () => {
  const plan = wrapper.spawnPlan({
    packageRoot: "/pkg",
    argv: ["setup"],
    platform: "linux",
    arch: "x64",
    env: { PATH: "/bin" },
  });

  assert.equal(plan.command, path.join("/pkg", "vendor", "remorkd-linux-amd64"));
  assert.deepEqual(plan.args, ["setup"]);
  assert.equal(plan.options.stdio, "inherit");
});
```

- [ ] **Step 2: Create wrapper**

Create `npm/remorkd/bin/remorkd.js`:

```js
#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const childProcess = require("node:child_process");

function daemonBinaryName(runtime = process) {
  const platform = runtime.platform;
  const arch = runtime.arch;
  if (platform === "linux" && arch === "arm64") return "remorkd-linux-arm64";
  if (platform === "linux" && arch === "x64") return "remorkd-linux-amd64";
  throw new Error(`unsupported Remork daemon platform: ${platform}-${arch}`);
}

function packageRootFromFilename(filename = __filename) {
  return path.resolve(path.dirname(filename), "..");
}

function spawnPlan({
  packageRoot = packageRootFromFilename(),
  argv = process.argv.slice(2),
  platform = process.platform,
  arch = process.arch,
  env = process.env,
} = {}) {
  return {
    command: path.join(packageRoot, "vendor", daemonBinaryName({ platform, arch })),
    args: argv,
    options: {
      stdio: "inherit",
      env: { ...env },
    },
  };
}

function main() {
  let plan;
  try {
    plan = spawnPlan();
    if (!fs.existsSync(plan.command)) {
      throw new Error(`Remork daemon binary is missing: ${plan.command}`);
    }
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }
  const child = childProcess.spawn(plan.command, plan.args, plan.options);
  child.on("error", (err) => {
    console.error(err.message);
    process.exit(1);
  });
  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code ?? 1);
  });
}

module.exports = { daemonBinaryName, packageRootFromFilename, spawnPlan, main };

if (require.main === module) {
  main();
}
```

- [ ] **Step 3: Update package build script**

In `scripts/build-npm-package.sh`, add a second package directory:

```bash
server_pkg_dir="$repo_root/npm/remorkd"
server_vendor_dir="$server_pkg_dir/vendor"
```

After the client package generation, generate server package files:

```bash
rm -rf "$server_vendor_dir"
mkdir -p "$server_vendor_dir" "$server_pkg_dir/bin" "$server_pkg_dir/test"
cp "$dist_dir/remorkd-linux-arm64" "$server_vendor_dir/remorkd-linux-arm64"
cp "$dist_dir/remorkd-linux-amd64" "$server_vendor_dir/remorkd-linux-amd64"
chmod 0755 "$server_vendor_dir"/remorkd-*
cp "$repo_root/npm/remorkd/bin/remorkd.js" "$server_pkg_dir/bin/remorkd.js"

cat > "$server_pkg_dir/package.json" <<EOF
{
  "name": "@zhangtao0408/remorkd",
  "version": "$npm_version",
  "description": "Remork server daemon for private Linux servers",
  "bin": {
    "remorkd": "bin/remorkd.js"
  },
  "os": [
    "linux"
  ],
  "cpu": [
    "arm64",
    "x64"
  ],
  "scripts": {
    "test": "node --test test/*.test.js"
  },
  "files": [
    "bin/",
    "vendor/",
    "README.md"
  ],
  "engines": {
    "node": ">=18"
  },
  "repository": {
    "type": "git",
    "url": "git+https://github.com/zhangtao0408/Remork.git"
  },
  "homepage": "https://github.com/zhangtao0408/Remork#readme",
  "bugs": {
    "url": "https://github.com/zhangtao0408/Remork/issues"
  },
  "license": "UNLICENSED"
}
EOF
```

Add server dry-run:

```bash
(cd "$server_pkg_dir" && npm test && npm pack --dry-run)
```

- [ ] **Step 4: Add server README**

Create `npm/remorkd/README.md`:

```markdown
# Remork Daemon

Server daemon for Remork remote workspace control.

## Install

```bash
npm install -g @zhangtao0408/remorkd@beta
remorkd setup
remorkd start
```

`remorkd setup` writes `~/.remork/remorkd.toml` and can generate a shared token.
Use the printed `remork connect --url http://HOST:PORT` command from your client
machine.

Do not expose `remorkd` directly to untrusted networks. Use token auth on shared
VPNs or multi-user networks.
```

- [ ] **Step 5: Run Node tests**

Run:

```bash
node --test npm/remork/test/*.test.js
node --test npm/remorkd/test/*.test.js
```

Expected: PASS.

- [ ] **Step 6: Commit Task 12**

```bash
git add npm/remorkd/bin/remorkd.js npm/remorkd/test/remorkd-wrapper.test.js npm/remorkd/README.md scripts/build-npm-package.sh
git commit -m "feat: package remorkd for npm"
```

---

### Task 13: Add Docs And E2E Coverage

**Files:**
- Modify: `README.md`
- Modify: `README_ZH.md`
- Modify: `npm/remork/README.md`
- Create: `test/e2e/remork_connect_e2e_test.go`

- [ ] **Step 1: Add e2e test**

Create `test/e2e/remork_connect_e2e_test.go`:

```go
package e2e

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"remork/internal/cli"
	"remork/internal/daemon"
)

func TestRemorkConnectExistingDaemonWorkflow(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	home := t.TempDir()
	mustWrite(t, filepath.Join(remote, "a.txt"), []byte("hello\n"))

	srv := httptest.NewServer(daemon.NewServer(daemon.Config{Roots: []string{remote}}).Handler())
	defer srv.Close()

	cmd := cli.NewRootCommand(cli.Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err := executeE2ECommand(cmd, "connect", "--url", srv.URL, "--host", "lab", "--workspace-path", "", "--first-sync=false", "--non-interactive")
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, out)
	}

	cmd = cli.NewRootCommand(cli.Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err = executeE2ECommand(cmd, "sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	cmd = cli.NewRootCommand(cli.Options{Version: "test", HomeDir: home, WorkingDir: local})
	out, err = executeE2ECommand(cmd, "run", "--remote-only", "--", "cat", "a.txt")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("run output = %q, want hello", out)
	}
}
```

Add this helper if the e2e package does not already have it:

```go
func executeE2ECommand(cmd *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
```

Add imports:

```go
import (
	"bytes"
	"strings"

	"github.com/spf13/cobra"
)
```

- [ ] **Step 2: Run e2e test to verify current state**

Run:

```bash
go test ./test/e2e -run TestRemorkConnectExistingDaemonWorkflow -count=1
```

Expected: PASS after Tasks 1-12.

- [ ] **Step 3: Update English README**

In `README.md`, add this under setup:

```markdown
### Connect to an existing daemon

If a server already runs `remorkd` and exposes an HTTP port, connect without SSH:

```bash
remork connect --url http://server:17731
```

The connect flow probes `/status`, asks for a token if the daemon requires one,
lets you choose or enter a workspace path inside the advertised allowed roots,
then writes the local host and workspace binding. After that, use the normal
commands:

```bash
remork sync
remork run -- pwd
remork shell
```
```

- [ ] **Step 4: Update Chinese README**

In `README_ZH.md`, add:

```markdown
### 连接已有 daemon

如果服务器上已经运行了 `remorkd`，并且 HTTP 端口可以从本机访问，可以不用 SSH 部署，直接连接：

```bash
remork connect --url http://server:17731
```

connect 会探测 `/status`，如果服务端需要 token 会提示输入；然后选择 allowed root 或输入其中的 workspace 路径，最后写入本机 host 配置和当前目录的 workspace binding。之后继续使用日常命令：

```bash
remork sync
remork run -- pwd
remork shell
```
```

- [ ] **Step 5: Update npm README**

In `npm/remork/README.md`, add:

```markdown
## Connect to an existing daemon

```bash
remork connect --url http://server:17731
```

Use this when `remorkd` is already running on a reachable server. The client
stores token auth in a local token file when needed.
```

- [ ] **Step 6: Run final verification**

Run:

```bash
go test ./...
node --test npm/remork/test/*.test.js
node --test npm/remorkd/test/*.test.js
git diff --check
```

Expected: PASS and no whitespace errors.

- [ ] **Step 7: Commit Task 13**

```bash
git add README.md README_ZH.md npm/remork/README.md test/e2e/remork_connect_e2e_test.go
git commit -m "docs: document existing daemon connect"
```

---

## Plan Self-Review

- Spec coverage:
  - `remork connect`: Tasks 4-6 and Task 13.
  - Token file default and rotation recovery: Tasks 1, 2, and 7.
  - Empty, relative, and absolute workspace path rules: Task 3 and Task 4.
  - Setup menu route: Task 6.
  - Server `remorkd setup`, `serve`, `start`, `stop`, `status`: Tasks 8-11.
  - Server npm package: Task 12.
  - Documentation and e2e verification: Task 13.
- Placeholder scan:
  - The plan contains no open-marker strings or intentionally vague implementation steps.
- Type consistency:
  - Host config uses `TokenFile string` and JSON key `token_file`.
  - Auth source uses `auth.TokenSource{Env, File}`.
  - Connect flow uses `ConnectSpec`, `ExecuteConnectSpec`, and `remoteroot.ResolveWorkspacePath`.
  - Server config uses `remorkdconfig.Config` with `ListenAddr`, `AllowedRoots`, `LargeFileThreshold`, `TokenFile`, `PIDFile`, and `LogFile`.
