# Remork Setup-First TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a setup-first human TUI for Remork while keeping advanced commands scriptable and single-sourcing deploy, host, init, and progress behavior.

**Architecture:** Extract command logic into typed operation specs that produce validated plans and then execute those plans. Add shared action/progress rendering, wire `remork setup` as a product orchestration layer over those specs, then update help, docs, and loading behavior across commands.

**Tech Stack:** Go, Cobra, Bubble Tea, Bubbles, Lip Gloss, existing Remork CLI test harness, remote validation via `z00879328_docker`.

---

## File Structure

- Create `internal/cli/daemon_deploy_spec.go`: typed daemon deploy spec, defaults, validation, plan generation, execution wrapper around existing daemon deploy helpers.
- Create `internal/cli/daemon_deploy_spec_test.go`: daemon spec defaults, validation, plan equivalence, and execution delegation tests.
- Create `internal/cli/host_spec.go`: typed host config spec and save/plan logic shared by `host add` and `setup`.
- Create `internal/cli/host_spec_test.go`: host URL validation, no-proxy/token-env persistence, JSON-safe behavior.
- Create `internal/cli/workspace_bind_spec.go`: typed workspace bind spec shared by `init` and `setup`.
- Create `internal/cli/workspace_bind_spec_test.go`: bind validation, advertised root checks, state directory creation.
- Create `internal/cli/setup.go`: `remork setup` command, scope/intent menu, minimal forms, review/execute orchestration.
- Create `internal/cli/setup_test.go`: setup scope selection, intent routing, generated specs, no accidental current-directory binding.
- Create `internal/tui/action.go`: shared action track model and renderer.
- Create `internal/tui/action_test.go`: queued/running/done/failed/skipped symbols, spinner frames, color/no-color behavior.
- Modify `internal/output/theme.go`: add productized clear rendering methods used by static output.
- Modify `internal/output/theme_test.go`: productized section/action/next rendering tests.
- Modify `internal/progress/progress.go`: use the shared action/progress vocabulary for text progress.
- Modify `internal/progress/progress_test.go`: sync-like counted progress expectations.
- Modify `internal/tui/progress.go`: replace line spinner with Remork spinner frames in rich TTY models.
- Modify `internal/tui/progress_test.go`: spinner frame and action-track rendering expectations.
- Modify `internal/cli/commands_daemon.go`: build `DaemonDeploySpec` and keep `daemon install/upgrade` as advanced entry points.
- Modify `internal/cli/commands_host.go`: route `host add` through `HostConfigSpec`; update empty-state next step to `remork setup`.
- Modify `internal/cli/commands_init.go`: route `init` through `WorkspaceBindSpec`.
- Modify `internal/cli/root.go`: add `setup`, promote setup-first help/menu grouping, move daemon/host/init to advanced discovery.
- Modify `internal/cli/commands_sync.go`, `commands_pull.go`, `commands_apply.go`, `commands_run.go`, `commands_watch.go`, `commands_shell.go`: adopt unified loading behavior without adding forms to daily commands.
- Modify `README.md`, `README_ZH.md`, `skills/remork/SKILL.md`: document setup-first human workflow and advanced primitives.

## Task 1: Daemon Deploy Spec

**Files:**
- Create: `internal/cli/daemon_deploy_spec.go`
- Create: `internal/cli/daemon_deploy_spec_test.go`
- Modify: `internal/cli/commands_daemon.go`

- [ ] **Step 1: Write failing spec plan test**

Add `internal/cli/daemon_deploy_spec_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestDaemonDeploySpecBuildsPlanForInstall(t *testing.T) {
	spec := DaemonDeploySpec{
		Action:     "install",
		HostName:   "lab",
		SSHTarget:  "lab.example",
		Roots:      []string{"/data/project"},
		Addr:       "127.0.0.1:17731",
		LocalBin:   fakeDaemonBinary(t),
		RemoteBin:  ".local/bin/remorkd",
		URL:        "http://lab.example:17731",
		TokenEnv:   "",
		NoProxy:    true,
		Verify:     true,
		Execute:    true,
		Confirmed:  true,
	}

	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		t.Fatalf("BuildDaemonDeployPlan: %v", err)
	}
	if plan.Title != "Daemon install" {
		t.Fatalf("title = %q, want Daemon install", plan.Title)
	}
	for _, want := range []string{
		"prepare remote directories",
		"stop existing remorkd daemon",
		"copy remorkd binary",
		"mark remorkd executable",
		"start remorkd daemon",
		"save host config",
		"verify daemon status",
	} {
		if !plan.HasAction(want) {
			t.Fatalf("plan actions missing %q: %#v", want, plan.Actions)
		}
	}
	if got := strings.Join(plan.Commands, "\n"); !strings.Contains(got, "scp") || !strings.Contains(got, ".local/bin/remorkd") {
		t.Fatalf("commands should include scp to remote bin, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestDaemonDeploySpecBuildsPlanForInstall -count=1
```

Expected: FAIL with undefined `DaemonDeploySpec` or `BuildDaemonDeployPlan`.

- [ ] **Step 3: Add plan types and minimal builder**

Create `internal/cli/daemon_deploy_spec.go` with:

```go
package cli

type OperationPlan struct {
	Title    string
	Target   map[string]string
	Actions  []PlannedAction
	Risks    []string
	Commands []string
	Next     []string
}

type PlannedAction struct {
	Label string
}

func (p OperationPlan) HasAction(label string) bool {
	for _, action := range p.Actions {
		if action.Label == label {
			return true
		}
	}
	return false
}

type DaemonDeploySpec struct {
	Action                          string
	HostName                        string
	SSHTarget                       string
	Roots                           []string
	Addr                            string
	LocalBin                        string
	RemoteBin                       string
	Platform                        string
	TokenFile                       string
	URL                             string
	TokenEnv                        string
	NoProxy                         bool
	Verify                          bool
	Execute                         bool
	Confirmed                       bool
	AllowUnauthenticatedNetworkBind bool
}

func BuildDaemonDeployPlan(spec DaemonDeploySpec) (OperationPlan, error) {
	deploy := daemonDeployOptions{
		action:                          spec.Action,
		hostName:                        spec.HostName,
		sshTarget:                       spec.SSHTarget,
		roots:                           append([]string(nil), spec.Roots...),
		addr:                            spec.Addr,
		localBin:                        spec.LocalBin,
		remoteBin:                       spec.RemoteBin,
		platform:                        spec.Platform,
		tokenFile:                       spec.TokenFile,
		url:                             spec.URL,
		tokenEnv:                        spec.TokenEnv,
		noProxy:                         spec.NoProxy,
		verify:                          spec.Verify,
		execute:                         spec.Execute,
		yes:                             spec.Confirmed,
		allowUnauthenticatedNetworkBind: spec.AllowUnauthenticatedNetworkBind,
	}
	applyDaemonDeployDefaults(&deploy)
	if err := validateDaemonDeployPlan(deploy); err != nil {
		return OperationPlan{}, err
	}
	if spec.Execute {
		if err := validateDaemonDeployExecution(deploy); err != nil {
			return OperationPlan{}, err
		}
	}
	remote := deploySSHTarget(deploy)
	plan := OperationPlan{
		Title: "Daemon " + deploy.action,
		Target: map[string]string{
			"host":       deploy.hostName,
			"remote":     remote,
			"remote_bin": remoteCommandPath(deploy.remoteBin),
		},
		Actions: []PlannedAction{
			{Label: "prepare remote directories"},
			{Label: "stop existing remorkd daemon"},
			{Label: "copy remorkd binary"},
			{Label: "mark remorkd executable"},
		},
		Commands: []string{
			"ssh " + shellQuote(remote) + " " + shellQuote(remotePrepareCommand(deploy)),
			"scp " + shellQuote(deploy.localBin) + " " + shellQuote(remote) + ":" + shellQuote(remoteSCPDestinationPath(deploy.remoteBin)),
			"ssh " + shellQuote(remote) + " " + shellQuote(remoteChmodCommand(deploy.remoteBin)),
		},
	}
	if remoteStartCommand(deploy) != "" {
		plan.Actions = append(plan.Actions, PlannedAction{Label: "start remorkd daemon"})
		plan.Commands = append(plan.Commands, "ssh "+shellQuote(remote)+" "+shellQuote(remoteStartCommand(deploy)))
	}
	if deploy.url != "" {
		plan.Actions = append(plan.Actions, PlannedAction{Label: "save host config"})
		plan.Next = append(plan.Next, "remork daemon status "+deploy.hostName)
	}
	if deploy.verify {
		plan.Actions = append(plan.Actions, PlannedAction{Label: "verify daemon status"})
	}
	if insecureNoTokenNonLoopbackAddr(deploy.addr, deploy.tokenFile != "") {
		plan.Risks = append(plan.Risks, "network bind without authentication")
	}
	return plan, nil
}

func applyDaemonDeployDefaults(deploy *daemonDeployOptions) {
	if deploy.action == "" {
		deploy.action = "install"
	}
	if deploy.addr == "" {
		deploy.addr = "0.0.0.0:17731"
	}
	if deploy.remoteBin == "" {
		deploy.remoteBin = ".local/bin/remorkd"
	}
}
```

- [ ] **Step 4: Run daemon spec test**

Run:

```bash
go test ./internal/cli -run TestDaemonDeploySpecBuildsPlanForInstall -count=1
```

Expected: PASS.

- [ ] **Step 5: Add validation equivalence test**

Append:

```go
func TestDaemonDeploySpecReusesNetworkBindValidation(t *testing.T) {
	_, err := BuildDaemonDeployPlan(DaemonDeploySpec{
		Action:    "install",
		HostName:  "lab",
		SSHTarget: "lab.example",
		Roots:     []string{"/data"},
		Addr:      "0.0.0.0:17731",
		LocalBin:  fakeDaemonBinary(t),
		RemoteBin: ".local/bin/remorkd",
		Execute:   true,
		Confirmed: true,
	})
	if err == nil || !strings.Contains(err.Error(), "without authentication") {
		t.Fatalf("BuildDaemonDeployPlan error = %v, want unauthenticated network bind validation", err)
	}
}
```

- [ ] **Step 6: Run validation test**

Run:

```bash
go test ./internal/cli -run TestDaemonDeploySpecReusesNetworkBindValidation -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/daemon_deploy_spec.go internal/cli/daemon_deploy_spec_test.go
git commit -m "refactor: add daemon deploy operation spec"
```

## Task 2: Host And Workspace Specs

**Files:**
- Create: `internal/cli/host_spec.go`
- Create: `internal/cli/host_spec_test.go`
- Create: `internal/cli/workspace_bind_spec.go`
- Create: `internal/cli/workspace_bind_spec_test.go`
- Modify: `internal/cli/commands_host.go`
- Modify: `internal/cli/commands_init.go`

- [ ] **Step 1: Write failing host spec test**

Create `internal/cli/host_spec_test.go`:

```go
package cli

import "testing"

func TestHostConfigSpecSavesHost(t *testing.T) {
	home := t.TempDir()
	spec := HostConfigSpec{
		Name:     "lab",
		URL:      "http://127.0.0.1:17731",
		TokenEnv: "REMORK_TOKEN",
		NoProxy:  true,
	}
	if _, err := PlanHostConfig(spec); err != nil {
		t.Fatalf("PlanHostConfig: %v", err)
	}
	if err := ExecuteHostConfigSpec(Options{HomeDir: home}, spec); err != nil {
		t.Fatalf("ExecuteHostConfigSpec: %v", err)
	}
	host, ok, err := loadConfiguredHost(Options{HomeDir: home}, "lab")
	if err != nil || !ok {
		t.Fatalf("loadConfiguredHost ok=%v err=%v", ok, err)
	}
	if host.URL != spec.URL || host.TokenEnv != spec.TokenEnv || !host.NoProxy {
		t.Fatalf("host = %#v, want spec %#v", host, spec)
	}
}
```

- [ ] **Step 2: Run host spec test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestHostConfigSpecSavesHost -count=1
```

Expected: FAIL with undefined `HostConfigSpec`.

- [ ] **Step 3: Implement host spec**

Create `internal/cli/host_spec.go`:

```go
package cli

import (
	"fmt"

	"remork/internal/config"
)

type HostConfigSpec struct {
	Name     string
	URL      string
	TokenEnv string
	NoProxy  bool
}

func PlanHostConfig(spec HostConfigSpec) (OperationPlan, error) {
	if spec.Name == "" {
		return OperationPlan{}, fmt.Errorf("host name is required")
	}
	if spec.URL == "" {
		return OperationPlan{}, fmt.Errorf("--url is required")
	}
	if err := validateDaemonURL(spec.URL); err != nil {
		return OperationPlan{}, err
	}
	return OperationPlan{
		Title: "Save host",
		Target: map[string]string{
			"name": spec.Name,
			"url":  spec.URL,
		},
		Actions: []PlannedAction{{Label: "save host config"}},
		Next:    []string{"remork daemon status " + spec.Name},
	}, nil
}

func ExecuteHostConfigSpec(opts Options, spec HostConfigSpec) error {
	if _, err := PlanHostConfig(spec); err != nil {
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
	cfg.Hosts[spec.Name] = config.Host{Name: spec.Name, URL: spec.URL, TokenEnv: spec.TokenEnv, NoProxy: spec.NoProxy}
	return store.Save(cfg)
}
```

- [ ] **Step 4: Run host spec test**

Run:

```bash
go test ./internal/cli -run TestHostConfigSpecSavesHost -count=1
```

Expected: PASS.

- [ ] **Step 5: Write failing workspace bind spec test**

Create `internal/cli/workspace_bind_spec_test.go`:

```go
package cli

import (
	"context"
	"testing"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/ops"
	"remork/internal/workspace"
)

type bindSpecProbe struct{}

func (bindSpecProbe) Status(context.Context, config.Host, string) (api.StatusResponse, error) {
	return api.StatusResponse{Roots: []string{"/data"}}, nil
}
func (bindSpecProbe) Manifest(context.Context, config.Host, config.Config, string) (api.ManifestResponse, error) {
	return api.ManifestResponse{Root: "/data/project"}, nil
}
func (bindSpecProbe) Operations(context.Context, config.Host, config.Config, string, int) ([]ops.Entry, error) {
	return nil, nil
}

func TestWorkspaceBindSpecWritesBinding(t *testing.T) {
	home := t.TempDir()
	local := t.TempDir()
	if err := ExecuteHostConfigSpec(Options{HomeDir: home}, HostConfigSpec{Name: "lab", URL: "http://127.0.0.1:17731"}); err != nil {
		t.Fatalf("host spec: %v", err)
	}
	spec := WorkspaceBindSpec{HostName: "lab", RemoteRoot: "/data/project", LocalRoot: local}
	if _, err := PlanWorkspaceBind(Options{HomeDir: home, WorkingDir: local, DaemonProbe: bindSpecProbe{}}, spec); err != nil {
		t.Fatalf("PlanWorkspaceBind: %v", err)
	}
	if err := ExecuteWorkspaceBindSpec(Options{HomeDir: home, WorkingDir: local, DaemonProbe: bindSpecProbe{}}, spec); err != nil {
		t.Fatalf("ExecuteWorkspaceBindSpec: %v", err)
	}
	binding, _, err := workspace.ResolveFrom(local)
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if binding.Host != "lab" || binding.RemoteRoot != "/data/project" {
		t.Fatalf("binding = %#v", binding)
	}
}
```

- [ ] **Step 6: Run workspace bind spec test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestWorkspaceBindSpecWritesBinding -count=1
```

Expected: FAIL with undefined `WorkspaceBindSpec`.

- [ ] **Step 7: Implement workspace bind spec by moving current init logic behind a shared function**

Create `internal/cli/workspace_bind_spec.go`:

```go
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type WorkspaceBindSpec struct {
	HostName   string
	RemoteRoot string
	LocalRoot  string
}

func PlanWorkspaceBind(opts Options, spec WorkspaceBindSpec) (OperationPlan, error) {
	if spec.HostName == "" || spec.RemoteRoot == "" {
		return OperationPlan{}, fmt.Errorf("host and workspace root are required")
	}
	return OperationPlan{
		Title: "Bind workspace",
		Target: map[string]string{
			"host":           spec.HostName,
			"workspace root": spec.RemoteRoot,
			"local root":     spec.LocalRoot,
		},
		Actions: []PlannedAction{
			{Label: "verify daemon status"},
			{Label: "verify workspace root"},
			{Label: "write workspace binding"},
		},
		Next: []string{"remork sync"},
	}, nil
}

func ExecuteWorkspaceBindSpec(opts Options, spec WorkspaceBindSpec) error {
	if _, err := PlanWorkspaceBind(opts, spec); err != nil {
		return err
	}
	workingDir := opts.WorkingDir
	if spec.LocalRoot != "" {
		opts.WorkingDir = spec.LocalRoot
	}
	if opts.WorkingDir == "" {
		opts.WorkingDir = workingDir
	}
	return initWorkspace(specCommand(), opts, spec.HostName, spec.RemoteRoot)
}

func specCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "workspace-bind-spec"}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}
```

- [ ] **Step 8: Run host and workspace tests**

Run:

```bash
go test ./internal/cli -run 'Test(HostConfigSpecSavesHost|WorkspaceBindSpecWritesBinding)' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/host_spec.go internal/cli/host_spec_test.go internal/cli/workspace_bind_spec.go internal/cli/workspace_bind_spec_test.go
git commit -m "refactor: add host and workspace operation specs"
```

## Task 3: Shared Action Track And Productized Renderer

**Files:**
- Create: `internal/tui/action.go`
- Create: `internal/tui/action_test.go`
- Modify: `internal/output/theme.go`
- Modify: `internal/output/theme_test.go`
- Modify: `internal/tui/progress.go`
- Modify: `internal/tui/progress_test.go`

- [ ] **Step 1: Write failing action track tests**

Create `internal/tui/action_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"remork/internal/output"
)

func TestActionTrackRendersSharedSymbols(t *testing.T) {
	track := ActionTrack{
		Title: "Actions",
		Actions: []ActionItem{
			{Label: "Build plan", State: ActionDone},
			{Label: "Apply changes", State: ActionRunning},
			{Label: "Refresh local state", State: ActionQueued},
			{Label: "Skipped optional sync", State: ActionSkipped},
			{Label: "Failed verify", State: ActionFailed},
		},
		SpinnerFrame: "O",
		Color:        output.ColorNever,
	}
	view := track.View()
	for _, want := range []string{
		"✓ Build plan",
		"O Apply changes",
		"· Refresh local state",
		"- Skipped optional sync",
		"× Failed verify",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestRemorkSpinnerFramesAreStable(t *testing.T) {
	want := []string{".", "o", "O", "°", "O", "o", "."}
	if got := RemorkSpinnerFrames(); strings.Join(got, "") != strings.Join(want, "") {
		t.Fatalf("frames = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run action tests to verify they fail**

Run:

```bash
go test ./internal/tui -run 'Test(ActionTrackRendersSharedSymbols|RemorkSpinnerFramesAreStable)' -count=1
```

Expected: FAIL with undefined `ActionTrack`.

- [ ] **Step 3: Implement action track**

Create `internal/tui/action.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"remork/internal/output"
)

type ActionState string

const (
	ActionQueued  ActionState = "queued"
	ActionRunning ActionState = "running"
	ActionDone    ActionState = "done"
	ActionFailed  ActionState = "failed"
	ActionSkipped ActionState = "skipped"
)

type ActionItem struct {
	Label string
	State ActionState
}

type ActionTrack struct {
	Title        string
	Actions      []ActionItem
	SpinnerFrame string
	Color        output.ColorMode
}

func RemorkSpinnerFrames() []string {
	return []string{".", "o", "O", "°", "O", "o", "."}
}

func (t ActionTrack) View() string {
	var b strings.Builder
	if t.Title != "" {
		fmt.Fprintf(&b, "%s\n", t.Title)
	}
	for _, action := range t.Actions {
		fmt.Fprintf(&b, "  %s %s\n", t.symbol(action.State), action.Label)
	}
	return b.String()
}

func (t ActionTrack) symbol(state ActionState) string {
	switch state {
	case ActionRunning:
		if t.SpinnerFrame != "" {
			return t.SpinnerFrame
		}
		return "."
	case ActionDone:
		return "✓"
	case ActionFailed:
		return "×"
	case ActionSkipped:
		return "-"
	default:
		return "·"
	}
}
```

- [ ] **Step 4: Run action tests**

Run:

```bash
go test ./internal/tui -run 'Test(ActionTrackRendersSharedSymbols|RemorkSpinnerFramesAreStable)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Change `NewProgressModel` to Remork spinner**

Modify `internal/tui/progress.go`:

```go
func NewProgressModel(title string) ProgressModel {
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: RemorkSpinnerFrames(),
		FPS:    time.Second / 8,
	}
	return ProgressModel{Title: title, State: StepLoading, spin: s}
}
```

Add import:

```go
import "time"
```

- [ ] **Step 6: Add spinner test**

Append to `internal/tui/progress_test.go`:

```go
func TestProgressModelUsesRemorkSpinner(t *testing.T) {
	model := NewProgressModel("Setup")
	frames := model.spin.Spinner.Frames
	if strings.Join(frames, "") != ".oO°Oo." {
		t.Fatalf("spinner frames = %#v", frames)
	}
}
```

- [ ] **Step 7: Run TUI tests**

Run:

```bash
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/action.go internal/tui/action_test.go internal/tui/progress.go internal/tui/progress_test.go
git commit -m "feat: add shared action progress vocabulary"
```

## Task 4: Setup Command Skeleton And Scope Menu

**Files:**
- Create: `internal/cli/setup.go`
- Create: `internal/cli/setup_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write failing root command test**

Append to `internal/cli/root_test.go`:

```go
func TestRootIncludesSetupCommand(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test"})
	setup, _, err := cmd.Find([]string{"setup"})
	if err != nil {
		t.Fatalf("find setup: %v", err)
	}
	if setup == nil || setup.Name() != "setup" {
		t.Fatalf("setup command = %#v", setup)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestRootIncludesSetupCommand -count=1
```

Expected: FAIL because `setup` is not registered.

- [ ] **Step 3: Add setup command skeleton**

Create `internal/cli/setup.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type setupScope string

const (
	setupScopeConnectProject setupScope = "connect_project"
	setupScopePrepareServer  setupScope = "prepare_server"
	setupScopeRepair         setupScope = "repair"
)

func addSetupCommand(root *cobra.Command, opts Options) {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up Remork for a server or workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := commandInteractionMode(cmd, interactionRequest{MissingInput: true})
			if !mode.Wizard {
				return codedCommandError{
					code: exitcode.InvalidUsageOrConfig,
					err:  fmt.Errorf("remork setup requires an interactive terminal"),
					fix:  "run remork setup in a terminal, or use advanced commands such as remork host add and remork init",
				}
			}
			return runSetupScopeMenu(cmd, opts)
		},
	}
	root.AddCommand(cmd)
}

func runSetupScopeMenu(cmd *cobra.Command, opts Options) error {
	plainRenderer(cmd, false).Section("Setup")
	plainRenderer(cmd, false).List("Choose what to set up", []string{
		"Connect this project",
		"Only prepare a server",
		"Repair an existing setup",
	})
	return nil
}
```

Add import in `setup.go`:

```go
"remork/internal/exitcode"
```

Modify `internal/cli/root.go` in `NewRootCommand`:

```go
addSetupCommand(root, opts)
addHostCommand(root, opts)
```

- [ ] **Step 4: Run setup root test**

Run:

```bash
go test ./internal/cli -run TestRootIncludesSetupCommand -count=1
```

Expected: PASS.

- [ ] **Step 5: Write scope model test**

Create `internal/cli/setup_test.go`:

```go
package cli

import "testing"

func TestSetupScopeDoesNotAssumeCurrentProject(t *testing.T) {
	items := setupScopeItems(false)
	if len(items) == 0 || items[0].Name != "Connect this project" {
		t.Fatalf("first setup scope = %#v", items)
	}
	foundPrepare := false
	for _, item := range items {
		if item.Name == "Only prepare a server" {
			foundPrepare = true
		}
	}
	if !foundPrepare {
		t.Fatalf("setup scopes should include server-only option: %#v", items)
	}
}
```

- [ ] **Step 6: Implement scope item helper**

Add to `internal/cli/setup.go`:

```go
func setupScopeItems(bound bool) []tui.CommandItem {
	if bound {
		return []tui.CommandItem{
			{Name: "Update an existing server", Description: "Update or verify the daemon used by this workspace", Args: []string{"update"}},
			{Name: "Repair an existing setup", Description: "Check host, daemon, auth, roots, and workspace binding", Args: []string{"repair"}},
			{Name: "Only prepare a server", Description: "Install or update remorkd without binding this directory", Args: []string{"prepare"}},
		}
	}
	return []tui.CommandItem{
		{Name: "Connect this project", Description: "Prepare or choose a daemon, bind this directory, then offer first sync", Args: []string{"connect"}},
		{Name: "Only prepare a server", Description: "Install or update remorkd and configure a host profile", Args: []string{"prepare"}},
		{Name: "Repair an existing setup", Description: "Check host, daemon, auth, roots, and workspace binding", Args: []string{"repair"}},
	}
}
```

Add import:

```go
"remork/internal/tui"
```

- [ ] **Step 7: Run setup tests**

Run:

```bash
go test ./internal/cli -run 'Test(RootIncludesSetupCommand|SetupScopeDoesNotAssumeCurrentProject)' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/setup.go internal/cli/setup_test.go internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: add setup command scope entry"
```

## Task 5: Setup Prepare Server Flow

**Files:**
- Modify: `internal/cli/setup.go`
- Modify: `internal/cli/setup_test.go`
- Modify: `internal/cli/commands_daemon.go`

- [ ] **Step 1: Write failing prepare spec test**

Append to `internal/cli/setup_test.go`:

```go
func TestSetupPrepareServerBuildsDaemonAndHostSpecs(t *testing.T) {
	values := map[string]string{
		"host":       "lab",
		"ssh":        "lab.example",
		"roots":      "/data",
		"url":        "http://lab.example:17731",
		"addr":       "127.0.0.1:17731",
		"local_bin":  "/tmp/remorkd",
		"remote_bin": ".local/bin/remorkd",
		"token_env":  "REMORK_TOKEN",
		"no_proxy":   "yes",
		"verify":     "yes",
	}
	spec, host, err := setupPrepareServerSpecs(values)
	if err != nil {
		t.Fatalf("setupPrepareServerSpecs: %v", err)
	}
	if spec.Action != "install" || spec.HostName != "lab" || spec.SSHTarget != "lab.example" {
		t.Fatalf("daemon spec = %#v", spec)
	}
	if host.Name != "lab" || host.URL != "http://lab.example:17731" || !host.NoProxy {
		t.Fatalf("host spec = %#v", host)
	}
}
```

- [ ] **Step 2: Run prepare spec test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestSetupPrepareServerBuildsDaemonAndHostSpecs -count=1
```

Expected: FAIL with undefined `setupPrepareServerSpecs`.

- [ ] **Step 3: Implement prepare server spec builder**

Add to `internal/cli/setup.go`:

```go
func setupPrepareServerSpecs(values map[string]string) (DaemonDeploySpec, HostConfigSpec, error) {
	verify, err := parseDaemonDeployBool(values["verify"], "verify")
	if err != nil {
		return DaemonDeploySpec{}, HostConfigSpec{}, err
	}
	noProxy, err := parseDaemonDeployBool(values["no_proxy"], "no proxy")
	if err != nil {
		return DaemonDeploySpec{}, HostConfigSpec{}, err
	}
	host := strings.TrimSpace(values["host"])
	url := strings.TrimSpace(values["url"])
	spec := DaemonDeploySpec{
		Action:    "install",
		HostName:  host,
		SSHTarget: strings.TrimSpace(values["ssh"]),
		Roots:     splitDaemonDeployRoots(values["roots"]),
		Addr:      strings.TrimSpace(values["addr"]),
		LocalBin:  strings.TrimSpace(values["local_bin"]),
		RemoteBin: strings.TrimSpace(values["remote_bin"]),
		URL:       url,
		TokenEnv:  strings.TrimSpace(values["token_env"]),
		NoProxy:   noProxy,
		Verify:    verify,
		Execute:   true,
	}
	return spec, HostConfigSpec{Name: host, URL: url, TokenEnv: spec.TokenEnv, NoProxy: noProxy}, nil
}
```

Add import:

```go
"strings"
```

- [ ] **Step 4: Run prepare spec test**

Run:

```bash
go test ./internal/cli -run TestSetupPrepareServerBuildsDaemonAndHostSpecs -count=1
```

Expected: PASS.

- [ ] **Step 5: Add prepare form fields test**

Append:

```go
func TestSetupPrepareServerFieldsAreMinimal(t *testing.T) {
	fields := setupPrepareServerFields(nil)
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	got := strings.Join(keys, ",")
	for _, want := range []string{"host", "ssh", "roots", "url", "addr", "token_env", "verify"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fields missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "allow_unauthenticated_network_bind") || strings.Contains(got, "dry_run") || strings.Contains(got, "yes") {
		t.Fatalf("default prepare fields should not expose advanced flags: %s", got)
	}
}
```

- [ ] **Step 6: Implement minimal prepare fields**

Add:

```go
func setupPrepareServerFields(initial map[string]string) []tui.Field {
	if initial == nil {
		initial = map[string]string{}
	}
	return []tui.Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab", Initial: initial["host"]},
		{Key: "ssh", Label: "SSH target", Placeholder: "user@server", Initial: initial["ssh"]},
		{Key: "roots", Label: "Allowed roots", Placeholder: "/absolute/allowed/root", Initial: initial["roots"]},
		{Key: "url", Label: "Daemon URL", Placeholder: "http://server:17731", Initial: initial["url"]},
		{Key: "addr", Label: "Listen addr", Placeholder: "0.0.0.0:17731", Initial: firstNonEmpty(initial["addr"], "0.0.0.0:17731")},
		{Key: "local_bin", Label: "Local binary", Placeholder: "auto", Initial: initial["local_bin"]},
		{Key: "remote_bin", Label: "Remote binary", Placeholder: ".local/bin/remorkd", Initial: firstNonEmpty(initial["remote_bin"], ".local/bin/remorkd")},
		{Key: "token_env", Label: "Token env", Placeholder: "REMORK_TOKEN", Initial: initial["token_env"]},
		{Key: "no_proxy", Label: "Bypass proxy y/N", Placeholder: "no", Initial: firstNonEmpty(initial["no_proxy"], "no")},
		{Key: "verify", Label: "Verify y/N", Placeholder: "yes", Initial: firstNonEmpty(initial["verify"], "yes")},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
```

- [ ] **Step 7: Run setup tests**

Run:

```bash
go test ./internal/cli -run 'TestSetupPrepareServer(BuildsDaemonAndHostSpecs|FieldsAreMinimal)' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/setup.go internal/cli/setup_test.go
git commit -m "feat: build setup prepare server specs"
```

## Task 6: Setup Connect Project And Update Server Flows

**Files:**
- Modify: `internal/cli/setup.go`
- Modify: `internal/cli/setup_test.go`

- [ ] **Step 1: Write failing connect project spec test**

Append:

```go
func TestSetupConnectProjectBuildsWorkspaceBindSpec(t *testing.T) {
	values := map[string]string{
		"host":        "lab",
		"remote_root": "/data/project",
		"first_sync":  "yes",
	}
	bind, firstSync, err := setupConnectProjectSpec("/local/project", values)
	if err != nil {
		t.Fatalf("setupConnectProjectSpec: %v", err)
	}
	if bind.HostName != "lab" || bind.RemoteRoot != "/data/project" || bind.LocalRoot != "/local/project" {
		t.Fatalf("bind spec = %#v", bind)
	}
	if !firstSync {
		t.Fatal("firstSync should be true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestSetupConnectProjectBuildsWorkspaceBindSpec -count=1
```

Expected: FAIL with undefined `setupConnectProjectSpec`.

- [ ] **Step 3: Implement connect project spec builder**

Add:

```go
func setupConnectProjectSpec(localRoot string, values map[string]string) (WorkspaceBindSpec, bool, error) {
	firstSync, err := parseDaemonDeployBool(values["first_sync"], "first sync")
	if err != nil {
		return WorkspaceBindSpec{}, false, err
	}
	spec := WorkspaceBindSpec{
		HostName:   strings.TrimSpace(values["host"]),
		RemoteRoot: strings.TrimSpace(values["remote_root"]),
		LocalRoot:  localRoot,
	}
	if spec.HostName == "" || spec.RemoteRoot == "" {
		return WorkspaceBindSpec{}, false, fmt.Errorf("host and remote workspace root are required")
	}
	return spec, firstSync, nil
}
```

- [ ] **Step 4: Run connect project spec test**

Run:

```bash
go test ./internal/cli -run TestSetupConnectProjectBuildsWorkspaceBindSpec -count=1
```

Expected: PASS.

- [ ] **Step 5: Write failing update server spec test**

Append:

```go
func TestSetupUpdateServerUsesUpgradeAction(t *testing.T) {
	values := map[string]string{
		"host":       "lab",
		"ssh":        "lab.example",
		"roots":      "/data",
		"url":        "http://lab.example:17731",
		"addr":       "127.0.0.1:17731",
		"local_bin":  "/tmp/remorkd",
		"remote_bin": ".local/bin/remorkd",
		"verify":     "yes",
	}
	spec, err := setupUpdateServerSpec(values)
	if err != nil {
		t.Fatalf("setupUpdateServerSpec: %v", err)
	}
	if spec.Action != "upgrade" || !spec.Verify {
		t.Fatalf("update spec = %#v", spec)
	}
}
```

- [ ] **Step 6: Implement update server spec builder**

Add:

```go
func setupUpdateServerSpec(values map[string]string) (DaemonDeploySpec, error) {
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		return DaemonDeploySpec{}, err
	}
	spec.Action = "upgrade"
	return spec, nil
}
```

- [ ] **Step 7: Run setup flow tests**

Run:

```bash
go test ./internal/cli -run 'TestSetup(ConnectProjectBuildsWorkspaceBindSpec|UpdateServerUsesUpgradeAction)' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/setup.go internal/cli/setup_test.go
git commit -m "feat: add setup connect and update specs"
```

## Task 7: Setup Execution And Review Plan

**Files:**
- Modify: `internal/cli/setup.go`
- Modify: `internal/cli/setup_test.go`
- Modify: `internal/output/theme.go`
- Modify: `internal/output/theme_test.go`

- [ ] **Step 1: Write failing operation plan render test**

Append to `internal/output/theme_test.go`:

```go
func TestPlainThemeRendersProductizedActionPlan(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlainRenderer(&buf, PlainOptions{Color: ColorNever})
	r.ProductTitle("Setup plan", "Remote server will be prepared and verified.")
	r.KeyValue("host", "lab")
	r.ActionList("Actions", []string{"Prepare remote directories", "Copy remorkd binary"})
	r.Next([]string{"remork init lab:/data/project"})

	got := buf.String()
	for _, want := range []string{"Setup plan", "Remote server will be prepared", "host", "Actions", "1. Prepare remote directories", "Next", "remork init"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: Run renderer test to verify it fails**

Run:

```bash
go test ./internal/output -run TestPlainThemeRendersProductizedActionPlan -count=1
```

Expected: FAIL with undefined methods.

- [ ] **Step 3: Implement productized renderer helpers**

Add to `internal/output/theme.go`:

```go
func (r *PlainRenderer) ProductTitle(title, subtitle string) {
	if r.skip() {
		return
	}
	fmt.Fprintf(r.w, "%s\n", r.emphasis(title))
	if strings.TrimSpace(subtitle) != "" {
		fmt.Fprintf(r.w, "%s\n", r.dim(subtitle))
	}
}

func (r *PlainRenderer) ActionList(title string, actions []string) {
	if r.skip() {
		return
	}
	if strings.TrimSpace(title) != "" {
		fmt.Fprintf(r.w, "%s\n", r.emphasis(title))
	}
	for i, action := range actions {
		fmt.Fprintf(r.w, "  %d. %s\n", i+1, action)
	}
}

func (r *PlainRenderer) Next(commands []string) {
	if r.skip() || len(commands) == 0 {
		return
	}
	fmt.Fprintf(r.w, "%s\n", r.emphasis("Next"))
	for _, command := range commands {
		fmt.Fprintf(r.w, "  %s\n", r.colorize(ansiMagenta, command))
	}
}
```

- [ ] **Step 4: Run output tests**

Run:

```bash
go test ./internal/output -count=1
```

Expected: PASS.

- [ ] **Step 5: Write setup review render test**

Append to `internal/cli/setup_test.go`:

```go
func TestRenderSetupPlanIncludesActionsAndNext(t *testing.T) {
	var buf bytes.Buffer
	plan := OperationPlan{
		Title: "Setup plan",
		Target: map[string]string{"host": "lab"},
		Actions: []PlannedAction{{Label: "prepare remote directories"}, {Label: "copy remorkd binary"}},
		Next: []string{"remork init lab:/data/project"},
	}
	renderSetupPlan(&buf, output.ColorNever, plan)
	got := buf.String()
	for _, want := range []string{"Setup plan", "host", "prepare remote directories", "copy remorkd binary", "remork init"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered setup plan missing %q:\n%s", want, got)
		}
	}
}
```

Add imports:

```go
"bytes"
"remork/internal/output"
```

- [ ] **Step 6: Implement setup plan renderer**

Add to `internal/cli/setup.go`:

```go
func renderSetupPlan(w interface{ Write([]byte) (int, error) }, color output.ColorMode, plan OperationPlan) {
	r := output.NewPlainRenderer(w, output.PlainOptions{Color: color})
	r.ProductTitle(plan.Title, "Review what Remork will do before it changes anything.")
	keys := make([]string, 0, len(plan.Target))
	for key := range plan.Target {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		r.KeyValue(key, plan.Target[key])
	}
	actions := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		actions = append(actions, action.Label)
	}
	r.ActionList("Actions", actions)
	if len(plan.Risks) > 0 {
		r.List("Risks", plan.Risks)
	}
	r.Next(plan.Next)
}
```

Add imports:

```go
"sort"
"remork/internal/output"
```

- [ ] **Step 7: Run setup render test**

Run:

```bash
go test ./internal/cli -run TestRenderSetupPlanIncludesActionsAndNext -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/output/theme.go internal/output/theme_test.go internal/cli/setup.go internal/cli/setup_test.go
git commit -m "feat: render setup review plans"
```

## Task 8: Setup Interactive Orchestration

**Files:**
- Modify: `internal/cli/setup.go`
- Modify: `internal/cli/setup_test.go`

- [ ] **Step 1: Write failing non-interactive setup test**

Append to `internal/cli/setup_test.go`:

```go
func TestSetupNonInteractiveReturnsAdvancedCommandGuidance(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test", HomeDir: t.TempDir(), WorkingDir: t.TempDir()})
	_, err := executeCommand(cmd, "setup", "--non-interactive")
	if err == nil {
		t.Fatal("setup --non-interactive should fail")
	}
	for _, want := range []string{"interactive terminal", "remork host add", "remork init"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("setup error should contain %q, got %v", want, err)
		}
	}
}
```

- [ ] **Step 2: Run test**

Run:

```bash
go test ./internal/cli -run TestSetupNonInteractiveReturnsAdvancedCommandGuidance -count=1
```

Expected: PASS after Task 4 skeleton. If it fails, update the setup command error text to include both `remork host add` and `remork init`.

- [ ] **Step 3: Write failing prepare execution helper test**

Append:

```go
func TestSetupPrepareServerExecutionRendersPlanBeforeExecute(t *testing.T) {
	localBin := fakeDaemonBinary(t)
	values := map[string]string{
		"host":       "lab",
		"ssh":        "lab.example",
		"roots":      "/data",
		"url":        "http://lab.example:17731",
		"addr":       "127.0.0.1:17731",
		"local_bin":  localBin,
		"remote_bin": ".local/bin/remorkd",
		"no_proxy":   "yes",
		"verify":     "no",
	}
	var out bytes.Buffer
	err := executeSetupPrepareServerPlan(&out, output.ColorNever, values)
	if err != nil {
		t.Fatalf("executeSetupPrepareServerPlan: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Daemon install", "lab", "prepare remote directories", "copy remorkd binary"} {
		if !strings.Contains(got, want) {
			t.Fatalf("setup prepare plan missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 4: Implement prepare review helper**

Add to `internal/cli/setup.go`:

```go
func executeSetupPrepareServerPlan(w interface{ Write([]byte) (int, error) }, color output.ColorMode, values map[string]string) error {
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		return err
	}
	spec.Confirmed = true
	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		return err
	}
	renderSetupPlan(w, color, plan)
	return nil
}
```

- [ ] **Step 5: Run prepare review helper test**

Run:

```bash
go test ./internal/cli -run TestSetupPrepareServerExecutionRendersPlanBeforeExecute -count=1
```

Expected: PASS.

- [ ] **Step 6: Replace setup scope stub with real menu dispatch**

Modify `runSetupScopeMenu` in `internal/cli/setup.go`:

```go
func runSetupScopeMenu(cmd *cobra.Command, opts Options) error {
	model := tui.NewCommandMenu("Remork setup", setupScopeItems(workspaceIsBound(opts)))
	model.Color = commandColorMode(cmd)
	menu, err := tui.RunCommandMenu(model, tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.ErrOrStderr()))
	if err != nil {
		return err
	}
	if menu.Canceled() || !menu.Submitted() {
		return nil
	}
	args := menu.SelectedArgs()
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "prepare":
		return runSetupPrepareServer(cmd, opts)
	case "connect":
		return runSetupConnectProject(cmd, opts)
	case "update":
		return runSetupUpdateServer(cmd, opts)
	case "repair":
		return runSetupRepair(cmd, opts)
	default:
		return fmt.Errorf("unknown setup scope %q", args[0])
	}
}
```

Add imports:

```go
tea "github.com/charmbracelet/bubbletea"
```

- [ ] **Step 7: Implement prepare, connect, update, and repair command handlers**

Add:

```go
func runSetupPrepareServer(cmd *cobra.Command, opts Options) error {
	values, err := runTUIForm(cmd, "Prepare server", setupPrepareServerFields(nil))
	if err != nil {
		return err
	}
	spec, _, err := setupPrepareServerSpecs(values)
	if err != nil {
		return err
	}
	spec.Confirmed = false
	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		return err
	}
	renderSetupPlan(cmd.OutOrStdout(), commandColorMode(cmd), plan)
	ok, err := prompt.Confirm(prompt.Options{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr()}, "execute setup plan?")
	if err != nil || !ok {
		return err
	}
	deploy := daemonDeployOptions{
		action: spec.Action, hostName: spec.HostName, sshTarget: spec.SSHTarget,
		roots: spec.Roots, addr: spec.Addr, localBin: spec.LocalBin,
		remoteBin: spec.RemoteBin, tokenFile: spec.TokenFile, url: spec.URL,
		tokenEnv: spec.TokenEnv, noProxy: spec.NoProxy, verify: spec.Verify,
		execute: true, yes: true, allowUnauthenticatedNetworkBind: spec.AllowUnauthenticatedNetworkBind,
		probe: opts.DaemonProbe, version: opts.Version, ctx: cmd.Context(),
		color: commandColorMode(cmd), canPrompt: true, confirmIn: cmd.InOrStdin(), confirmOut: cmd.ErrOrStderr(),
	}
	deploy.store, err = configStore(opts)
	if err != nil {
		return err
	}
	deploy.storeReady = true
	if deploy.runner == nil {
		deploy.runner = opts.CommandRunner
	}
	if deploy.runner == nil {
		deploy.runner = osCommandRunner{}
	}
	return runDaemonDeploy(cmd.OutOrStdout(), deploy)
}

func runSetupConnectProject(cmd *cobra.Command, opts Options) error {
	localRoot, err := filepath.Abs(opts.WorkingDir)
	if err != nil {
		return err
	}
	values, err := runTUIForm(cmd, "Connect this project", []tui.Field{
		{Key: "host", Label: "Host", Placeholder: "my-lab"},
		{Key: "remote_root", Label: "Remote workspace root", Placeholder: "/absolute/remote/workspace"},
		{Key: "first_sync", Label: "Run first sync y/N", Placeholder: "yes", Initial: "yes"},
	})
	if err != nil {
		return err
	}
	spec, firstSync, err := setupConnectProjectSpec(localRoot, values)
	if err != nil {
		return err
	}
	if err := ExecuteWorkspaceBindSpec(opts, spec); err != nil {
		return err
	}
	if firstSync {
		cmd.Root().SetArgs([]string{"sync"})
		return cmd.Root().ExecuteContext(cmd.Context())
	}
	plainRenderer(cmd, false).Next([]string{"remork sync"})
	return nil
}

func runSetupUpdateServer(cmd *cobra.Command, opts Options) error {
	values, err := runTUIForm(cmd, "Update server", setupPrepareServerFields(nil))
	if err != nil {
		return err
	}
	spec, err := setupUpdateServerSpec(values)
	if err != nil {
		return err
	}
	spec.Confirmed = false
	plan, err := BuildDaemonDeployPlan(spec)
	if err != nil {
		return err
	}
	renderSetupPlan(cmd.OutOrStdout(), commandColorMode(cmd), plan)
	return nil
}

func runSetupRepair(cmd *cobra.Command, opts Options) error {
	plainRenderer(cmd, false).ProductTitle("Repair setup", "Run remork doctor to inspect host, daemon, auth, roots, and workspace binding.")
	plainRenderer(cmd, false).Next([]string{"remork doctor"})
	return nil
}
```

Add imports:

```go
"path/filepath"
"remork/internal/prompt"
```

- [ ] **Step 8: Run setup tests**

Run:

```bash
go test ./internal/cli -run 'TestSetup(NonInteractiveReturnsAdvancedCommandGuidance|PrepareServerExecutionRendersPlanBeforeExecute|PrepareServerBuildsDaemonAndHostSpecs|ConnectProjectBuildsWorkspaceBindSpec|UpdateServerUsesUpgradeAction|ScopeDoesNotAssumeCurrentProject)' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/cli/setup.go internal/cli/setup_test.go
git commit -m "feat: wire setup interactive orchestration"
```

## Task 9: Root Help And Menu Setup-First Discovery

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/tui/menu_test.go`

- [ ] **Step 1: Write failing help test**

Modify or add in `internal/cli/root_test.go`:

```go
func TestRootHelpPromotesSetupFirst(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test"}), "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	text := out.String()
	mustContain(t, text, "Setup:")
	mustContain(t, text, "setup")
	if strings.Index(text, "setup") > strings.Index(text, "daemon") {
		t.Fatalf("setup should appear before daemon in help:\n%s", text)
	}
}
```

- [ ] **Step 2: Run help test to verify it fails**

Run:

```bash
go test ./internal/cli -run TestRootHelpPromotesSetupFirst -count=1
```

Expected: FAIL because current help says `Must know: init sync status apply run shell`.

- [ ] **Step 3: Update help template**

Modify `productHelpTemplate` in `internal/cli/root.go`:

```go
const productHelpTemplate = `{{.Short}}

Usage:
  {{.UseLine}}

Setup:
  setup       Set up Remork for a server or workspace

Daily:
  sync        Sync remote files into the local working copy
  status      Show local, remote, conflict, and large-file state
  diff        Show local changes against the synced base
  apply       Write local changes to the remote after base checks
  pull        Fetch a specific file or directory
  run         Run a command in the remote workspace
  shell       Open an interactive remote shell

Observe:
  log         Show recent remote Remork operations
  watch       Keep syncing from remote events

Diagnose:
  doctor      Check local and remote readiness

Advanced:
  daemon      Install, upgrade, or inspect remorkd
  host        Manage daemon endpoints
  workspace   Inspect or remove local bindings
  debug       Inspect daemon APIs and events
  init        Bind the current directory to a remote workspace

Other:
  version     Print the remork version
{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}{{if .HasAvailableInheritedFlags}}
Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}
`
```

- [ ] **Step 4: Update root command menu items**

Modify `rootCommandItems` in `internal/cli/root.go` so unbound workspaces start with setup:

```go
setup := []tui.CommandItem{
	{Group: "Setup", Name: "setup", Description: "Set up Remork for a server or workspace", Args: []string{"setup"}},
}
daily := []tui.CommandItem{
	{Group: "Daily", Name: "sync", Description: "Pull remote changes into this working copy", Args: []string{"sync"}},
	{Group: "Daily", Name: "status", Description: "Inspect local edits, remote updates, conflicts, and large files", Args: []string{"status"}},
	{Group: "Daily", Name: "diff", Description: "Review local edits before applying", Args: []string{"diff"}},
	{Group: "Daily", Name: "apply", Description: "Write reviewed local edits back to the remote", Args: []string{"apply"}},
	{Group: "Daily", Name: "pull", Description: "Fetch one file or directory, including large files", Args: []string{"pull"}, HelpOnly: true},
	{Group: "Daily", Name: "run", Description: "Run a non-interactive remote command", Args: []string{"run"}, HelpOnly: true},
	{Group: "Daily", Name: "shell", Description: "Open an interactive remote shell", Args: []string{"shell"}},
}
observe := []tui.CommandItem{
	{Group: "Observe", Name: "log", Description: "Show recent remote Remork operations", Args: []string{"log"}},
	{Group: "Observe", Name: "watch", Description: "Follow remote workspace events and sync updates", Args: []string{"watch"}},
}
diagnose := []tui.CommandItem{
	{Group: "Diagnose", Name: "doctor", Description: "Check local config, daemon reachability, and workspace APIs", Args: []string{"doctor"}},
}
advanced := []tui.CommandItem{
	{Group: "Advanced", Name: "daemon status", Description: "Inspect remorkd version, allowed roots, auth, and threshold", Args: []string{"daemon", "status"}, HelpOnly: true},
	{Group: "Advanced", Name: "daemon install", Description: "Install remorkd over SSH", Args: []string{"daemon", "install"}, HelpOnly: true},
	{Group: "Advanced", Name: "daemon upgrade", Description: "Upgrade remorkd over SSH", Args: []string{"daemon", "upgrade"}, HelpOnly: true},
	{Group: "Advanced", Name: "host list", Description: "List configured daemon endpoints", Args: []string{"host", "list"}},
	{Group: "Advanced", Name: "workspace", Description: "Inspect this local workspace binding", Args: []string{"workspace"}},
	{Group: "Advanced", Name: "init", Description: "Bind this directory manually", Args: []string{"init"}, HelpOnly: true},
}
if !bound {
	return append(append(append(append(setup, daily...), observe...), diagnose...), advanced...)
}
return append(append(append(append(daily, setup...), observe...), diagnose...), advanced...)
```

- [ ] **Step 5: Run root help/menu tests**

Run:

```bash
go test ./internal/cli -run 'TestRoot(HelpPromotesSetupFirst|IncludesSetupCommand)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: promote setup in help and menu"
```

## Task 10: Loading Coverage For Daily Commands

**Files:**
- Modify: `internal/progress/progress.go`
- Modify: `internal/progress/progress_test.go`
- Modify: `internal/cli/commands_sync.go`
- Modify: `internal/cli/commands_pull.go`
- Modify: `internal/cli/commands_apply.go`
- Modify: `internal/cli/commands_run.go`
- Modify: `internal/cli/commands_watch.go`
- Modify: `internal/cli/commands_shell.go`
- Modify tests under `internal/cli/*_test.go`

- [ ] **Step 1: Write failing text reporter test**

Append to `internal/progress/progress_test.go`:

```go
func TestTextReporterUsesSharedRunningSymbol(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, Options{Color: output.ColorNever})
	r.Start("sync: applying remote changes", 3)
	r.Advance(1)
	r.Done()
	got := buf.String()
	for _, want := range []string{"sync: applying remote changes", "1/3", "✓"} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress output missing %q:\n%s", want, got)
		}
	}
}
```

Add imports if missing:

```go
"bytes"
"strings"
"remork/internal/output"
```

- [ ] **Step 2: Run reporter test to verify it fails**

Run:

```bash
go test ./internal/progress -run TestTextReporterUsesSharedRunningSymbol -count=1
```

Expected: FAIL because current success marker is `ok`, not `✓`.

- [ ] **Step 3: Add static action methods to output renderer**

Add to `internal/output/theme.go`:

```go
func (r *PlainRenderer) ActionQueued(label string) {
	if !r.skip() {
		fmt.Fprintf(r.w, "%s %s\n", r.dim("·"), label)
	}
}

func (r *PlainRenderer) ActionRunning(label string) {
	if !r.skip() {
		fmt.Fprintf(r.w, "%s %s\n", r.colorize(ansiCyan, "."), label)
	}
}

func (r *PlainRenderer) ActionDone(label string) {
	if !r.skip() {
		fmt.Fprintf(r.w, "%s %s\n", r.dim("✓"), label)
	}
}

func (r *PlainRenderer) ActionFailed(label string) {
	if !r.skip() {
		fmt.Fprintf(r.w, "%s %s\n", r.colorize(ansiRed, "×"), label)
	}
}
```

- [ ] **Step 4: Update text reporter**

Modify `internal/progress/progress.go`:

```go
func (r *TextReporter) Start(label string, total int64) {
	r.label = label
	r.total = total
	r.current = 0
	if r.quiet || r.w == nil {
		return
	}
	renderer := output.NewPlainRenderer(r.w, output.PlainOptions{Quiet: r.quiet, Color: r.color})
	if total > 1 {
		renderer.ActionRunning(fmt.Sprintf("%s 0/%d", label, total))
		return
	}
	renderer.ActionRunning(label)
}

func (r *TextReporter) Done() {
	r.current = r.total
	if r.quiet || r.w == nil {
		return
	}
	renderer := output.NewPlainRenderer(r.w, output.PlainOptions{Quiet: r.quiet, Color: r.color})
	if r.total > 1 {
		renderer.ActionDone(fmt.Sprintf("%s %d/%d", r.label, r.current, r.total))
		return
	}
	renderer.ActionDone(r.label)
}
```

- [ ] **Step 5: Run progress tests**

Run:

```bash
go test ./internal/output ./internal/progress -count=1
```

Expected: PASS after updating existing assertions from `ok` to `✓` where needed.

- [ ] **Step 6: Add run command loading test**

Append to `internal/cli/commands_run_test.go`:

```go
func TestRunShowsPreflightLoadingOnStderr(t *testing.T) {
	home, local := runCommandWorkspace(t)
	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home, WorkingDir: local}), "run", "--no-sync-check", "printf hi")
	if err != nil {
		t.Fatalf("run: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "remote command running") || strings.Contains(stdout.String(), "·") {
		t.Fatalf("loading must not mix into stdout: %q", stdout.String())
	}
}
```

- [ ] **Step 7: Keep command stdout clean**

In `internal/cli/commands_run.go`, ensure all loading/status calls use `plainErrRenderer(cmd, false)` and never `plainRenderer(cmd, false)` before command stdout is printed.

Use:

```go
plainErrRenderer(cmd, false).ActionRunning("remote command running; output is replayed after completion")
```

- [ ] **Step 8: Run daily command tests**

Run:

```bash
go test ./internal/cli ./internal/progress ./internal/output -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/output/theme.go internal/output/theme_test.go internal/progress/progress.go internal/progress/progress_test.go internal/cli/commands_sync.go internal/cli/commands_pull.go internal/cli/commands_apply.go internal/cli/commands_run.go internal/cli/commands_watch.go internal/cli/commands_shell.go internal/cli/*_test.go
git commit -m "feat: unify command loading output"
```

## Task 11: Docs And Remote Validation

**Files:**
- Modify: `README.md`
- Modify: `README_ZH.md`
- Modify: `skills/remork/SKILL.md`
- Create: `docs/remork-setup-first-tui-validation.md`

- [ ] **Step 1: Update README setup section**

In `README.md`, replace the daemon-first install walkthrough with a setup-first path:

```markdown
### 2. Set up a server or workspace

For humans, start with:

```bash
remork setup
```

The setup flow can prepare a remote server, update an existing daemon, connect
the current project, or repair an existing configuration. Advanced commands
remain available for scripts and troubleshooting:

```bash
remork daemon install HOST --root /absolute/root --url http://HOST:17731 -y
remork daemon upgrade HOST --root /absolute/root --url http://HOST:17731 -y
remork host add HOST --url http://HOST:17731
remork init HOST:/absolute/workspace
```
```

- [ ] **Step 2: Update Chinese README**

In `README_ZH.md`, add:

```markdown
### 2. 设置服务器或工作区

人类用户优先使用：

```bash
remork setup
```

`setup` 会先询问你是要连接当前项目、只准备服务器、更新已有服务器，还是修复现有配置。`daemon install`、`daemon upgrade`、`host add` 和 `init` 仍然保留，主要面向脚本、Agent 和排障。
```

- [ ] **Step 3: Update Remork skill**

In `skills/remork/SKILL.md`, make `remork setup` the first human setup command and keep advanced command snippets for non-interactive agents.

Add:

```markdown
For interactive human setup, prefer `remork setup`. For scripted agent setup,
use the advanced primitives directly: `remork daemon install`, `remork daemon
upgrade`, `remork host add`, and `remork init`.
```

- [ ] **Step 4: Build binaries**

Run:

```bash
go test ./...
go build -o /tmp/remork-setup-plan ./cmd/remork
go build -o /tmp/remorkd-setup-plan ./cmd/remorkd
```

Expected: all tests pass and both binaries build.

- [ ] **Step 5: Local CLI validation**

Run:

```bash
/tmp/remork-setup-plan --help
/tmp/remork-setup-plan setup --help
/tmp/remork-setup-plan daemon install --help
```

Expected:

- root help promotes `setup`.
- setup help explains interactive usage.
- daemon install remains available.

- [ ] **Step 6: Remote validation with `z00879328_docker`**

Run:

```bash
ssh -G z00879328_docker | sed -n '1,40p'
ssh z00879328_docker 'uname -s; uname -m; mkdir -p /home/z00879328/remork-setup-tui-validation'
/tmp/remork-setup-plan daemon install z00879328_docker_2.7 \
  --ssh z00879328_docker \
  --root /home/z00879328 \
  --url http://175.100.2.7:17731 \
  --addr 0.0.0.0:17731 \
  --local-bin /tmp/remorkd-setup-plan \
  --remote-bin .local/bin/remorkd \
  --token-file .remork/remork.token \
  --token-env REMORK_TOKEN \
  --verify \
  --no-proxy \
  -y
```

Expected:

- SSH target resolves.
- daemon deploy uses the shared action progress output.
- host config is saved.
- verify passes or reports an actionable network/auth error.

- [ ] **Step 7: Save validation report**

Create `docs/remork-setup-first-tui-validation.md`:

```markdown
# Remork Setup-First TUI Validation

Date: 2026-05-06

## Local

- `go test ./...`: PASS
- `go build ./cmd/remork`: PASS
- `go build ./cmd/remorkd`: PASS
- root help promotes `setup`: PASS
- advanced daemon commands remain available: PASS

## Remote: z00879328_docker

- `ssh -G z00879328_docker`: PASS
- remote platform detection: PASS
- daemon deploy against `/home/z00879328`: PASS
- daemon status verify: PASS

## Notes

Record any proxy, token, firewall, or remote process cleanup details here.
```

- [ ] **Step 8: Commit docs and validation**

```bash
git add README.md README_ZH.md skills/remork/SKILL.md docs/remork-setup-first-tui-validation.md
git commit -m "docs: document setup-first tui workflow"
```

## Final Verification

- [ ] **Step 1: Run focused tests**

```bash
go test ./internal/cli ./internal/tui ./internal/output ./internal/progress ./internal/syncer -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Inspect working tree**

```bash
git status --short
```

Expected: no uncommitted files from this implementation except intentionally preserved user work. Do not stage unrelated pre-existing changes.

- [ ] **Step 4: Summarize implementation**

Final response should include:

- setup-first command entry added
- advanced commands preserved
- shared operation specs added
- unified action/progress vocabulary added
- docs updated
- tests and `z00879328_docker` validation result
