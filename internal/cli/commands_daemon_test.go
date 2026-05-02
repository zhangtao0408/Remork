package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"remork/internal/api"
	"remork/internal/config"
	"remork/internal/ops"
)

type recordedCommand struct {
	name string
	args []string
}

type fakeCommandRunner struct {
	commands []recordedCommand
	failAt   int
}

func (f *fakeCommandRunner) Run(name string, args ...string) error {
	f.commands = append(f.commands, recordedCommand{name: name, args: append([]string(nil), args...)})
	if f.failAt > 0 && len(f.commands) == f.failAt {
		return fmt.Errorf("command failed")
	}
	return nil
}

func TestDaemonDeployPlanWarnsForNonLoopbackNoToken(t *testing.T) {
	var out bytes.Buffer
	printDaemonDeployPlan(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab-a",
		root:      "/data/project-a",
		addr:      "0.0.0.0:17731",
		localBin:  "dist/remorkd-linux-arm64",
		remoteBin: ".local/bin/remorkd",
	})

	got := out.String()
	for _, want := range []string{"WARNING", "remote command execution", "--token-file"} {
		if !strings.Contains(got, want) {
			t.Fatalf("deploy plan should contain %q, got:\n%s", want, got)
		}
	}
}

func TestRunDaemonDeployExecuteRunsCommandsInOrder(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}

	want := []recordedCommand{
		{name: "ssh", args: []string{"lab.example", remotePrepareCommand(daemonDeployOptions{remoteBin: ".local/bin/remorkd"})}},
		{name: "scp", args: []string{"dist/remorkd-linux-arm64", "lab.example:.local/bin/remorkd"}},
		{name: "ssh", args: []string{"lab.example", remoteChmodCommand(".local/bin/remorkd")}},
		{name: "ssh", args: []string{"lab.example", remoteStartCommand(daemonDeployOptions{
			root:      "/data/project",
			addr:      "127.0.0.1:17731",
			remoteBin: ".local/bin/remorkd",
		})}},
	}
	if len(fake.commands) != len(want) {
		t.Fatalf("ran %d commands, want %d: %#v", len(fake.commands), len(want), fake.commands)
	}
	for i := range want {
		if fake.commands[i].name != want[i].name || strings.Join(fake.commands[i].args, "\x00") != strings.Join(want[i].args, "\x00") {
			t.Fatalf("command %d = %#v, want %#v", i, fake.commands[i], want[i])
		}
	}
	if !strings.Contains(out.String(), "daemon deploy executed") {
		t.Fatalf("output should confirm execution, got:\n%s", out.String())
	}
}

func TestRemoteStartCommandUsesDurableHomePaths(t *testing.T) {
	got := remoteStartCommand(daemonDeployOptions{
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
	})
	for _, want := range []string{
		`"$HOME/.remork/run/remorkd.pid"`,
		`>"$HOME/.remork/log/remorkd.log"`,
		`nohup "$HOME/.local/bin/remorkd"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("start command should contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/tmp/remorkd") || strings.Contains(got, "/tmp/remorkd.pid") || strings.Contains(got, "/tmp/remorkd.log") {
		t.Fatalf("start command should not use /tmp paths:\n%s", got)
	}
	if strings.Contains(got, "&& nohup") {
		t.Fatalf("start command should not background the stop/start compound command:\n%s", got)
	}
	if !strings.Contains(got, `fi; nohup "$HOME/.local/bin/remorkd"`) {
		t.Fatalf("start command should run stop and start sequentially:\n%s", got)
	}
}

func TestRemoteStartCommandSupportsMultipleAllowedRoots(t *testing.T) {
	got := remoteStartCommand(daemonDeployOptions{
		roots:     []string{"/data", "/scratch"},
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
	})
	want := `"$HOME/.local/bin/remorkd" '--root' '/data' '--root' '/scratch' '--addr' '127.0.0.1:17731'`
	if !strings.Contains(got, want) {
		t.Fatalf("start command should contain repeated roots %q, got:\n%s", want, got)
	}
}

func TestRemoteChmodCommandUsesDurableHomePath(t *testing.T) {
	got := remoteChmodCommand(".local/bin/remorkd")
	want := `chmod 0755 "$HOME/.local/bin/remorkd"`
	if got != want {
		t.Fatalf("remoteChmodCommand = %q, want %q", got, want)
	}
}

func TestRemoteSCPDestinationPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "default", path: "", want: ".local/bin/remorkd"},
		{name: "relative", path: ".local/bin/remorkd", want: ".local/bin/remorkd"},
		{name: "home env", path: "$HOME/bin/remorkd", want: "~/bin/remorkd"},
		{name: "home tilde", path: "~/bin/remorkd", want: "~/bin/remorkd"},
		{name: "absolute", path: "/opt/remorkd", want: "/opt/remorkd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remoteSCPDestinationPath(tt.path); got != tt.want {
				t.Fatalf("remoteSCPDestinationPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestRunDaemonDeployQuotesRemoteChmodPath(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: "/tmp/remork d;touch x",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	if len(fake.commands) < 4 {
		t.Fatalf("ran %d commands, want chmod and start commands: %#v", len(fake.commands), fake.commands)
	}

	chmod := fake.commands[2]
	want := "chmod 0755 '/tmp/remork d;touch x'"
	if chmod.name != "ssh" || len(chmod.args) != 2 || chmod.args[1] != want {
		t.Fatalf("chmod command = %#v, want %q", chmod, want)
	}
	if strings.Contains(chmod.args[1], "chmod 0755 /tmp/remork d;touch x") {
		t.Fatalf("chmod command contains unquoted remote path injection: %q", chmod.args[1])
	}
	if !strings.Contains(out.String(), "chmod 0755") || !strings.Contains(out.String(), "remork d;touch x") {
		t.Fatalf("deploy plan should print chmod for remote path, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), shellQuote("chmod 0755 /tmp/remork d;touch x")) {
		t.Fatalf("deploy plan contains unquoted remote path injection:\n%s", out.String())
	}
}

func TestRunDaemonDeployUpgradeRunsChmodWithoutStart(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "upgrade",
		hostName:  "lab",
		sshTarget: "lab.example",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	want := []recordedCommand{
		{name: "ssh", args: []string{"lab.example", remotePrepareCommand(daemonDeployOptions{remoteBin: ".local/bin/remorkd"})}},
		{name: "scp", args: []string{"dist/remorkd-linux-arm64", "lab.example:.local/bin/remorkd"}},
		{name: "ssh", args: []string{"lab.example", remoteChmodCommand(".local/bin/remorkd")}},
	}
	if len(fake.commands) != len(want) {
		t.Fatalf("ran %d commands, want %d: %#v", len(fake.commands), len(want), fake.commands)
	}
	for i := range want {
		if fake.commands[i].name != want[i].name || strings.Join(fake.commands[i].args, "\x00") != strings.Join(want[i].args, "\x00") {
			t.Fatalf("command %d = %#v, want %#v", i, fake.commands[i], want[i])
		}
	}
}

func TestRunDaemonDeployExecuteRequiresYes(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		runner:    fake,
	})
	if err == nil || !strings.Contains(err.Error(), "--execute requires --yes") {
		t.Fatalf("runDaemonDeploy error = %v, want --execute requires --yes", err)
	}
	if len(fake.commands) != 0 {
		t.Fatalf("ran commands despite missing --yes: %#v", fake.commands)
	}
}

func TestRunDaemonDeployStopsAfterCommandFailure(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{failAt: 2}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err == nil {
		t.Fatal("runDaemonDeploy returned nil error, want command failure")
	}
	if len(fake.commands) != 2 {
		t.Fatalf("ran %d commands, want stop after 2: %#v", len(fake.commands), fake.commands)
	}
}

func TestRunDaemonDeployExecuteConfiguresHostAndVerifies(t *testing.T) {
	var out bytes.Buffer
	fakeRunner := &fakeCommandRunner{}
	probe := &recordingDaemonProbe{}
	probe.roots = []string{"/data"}
	home := t.TempDir()

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:     "install",
		hostName:   "lab",
		sshTarget:  "lab.example",
		root:       "/data/project",
		addr:       "127.0.0.1:17731",
		remoteBin:  ".local/bin/remorkd",
		localBin:   "dist/remorkd-linux-arm64",
		url:        "http://lab.example:17731",
		tokenEnv:   "REMORK_TOKEN",
		noProxy:    true,
		verify:     true,
		execute:    true,
		yes:        true,
		store:      config.NewStore(filepath.Join(home, ".remork")),
		storeReady: true,
		probe:      probe,
		runner:     fakeRunner,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}

	cfg, err := config.NewStore(filepath.Join(home, ".remork")).Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	host := cfg.Hosts["lab"]
	if host.URL != "http://lab.example:17731" || host.TokenEnv != "REMORK_TOKEN" || !host.NoProxy {
		t.Fatalf("bad host config: %#v", host)
	}
	if probe.statusCalls != 1 || probe.statusHost.URL != "http://lab.example:17731" {
		t.Fatalf("status probe calls=%d host=%#v", probe.statusCalls, probe.statusHost)
	}
	for _, want := range []string{"host lab configured", "daemon status verified", "daemon deploy executed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output should contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestRunDaemonDeployVerifyRequiresAllAllowedRootsAdvertised(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data", "/scratch"}}
	home := t.TempDir()

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		roots:          []string{"/data/project", "/scratch/project"},
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       "dist/remorkd-linux-arm64",
		url:            "http://lab.example:17731",
		verify:         true,
		execute:        true,
		yes:            true,
		store:          config.NewStore(filepath.Join(home, ".remork")),
		storeReady:     true,
		probe:          probe,
		verifyTimeout:  50 * time.Millisecond,
		verifyInterval: time.Millisecond,
		runner:         &fakeCommandRunner{},
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
}

func TestRunDaemonDeployVerifyRejectsOneUnadvertisedRoot(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data"}}
	home := t.TempDir()

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		roots:          []string{"/data/project", "/scratch/project"},
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       "dist/remorkd-linux-arm64",
		url:            "http://lab.example:17731",
		verify:         true,
		execute:        true,
		yes:            true,
		store:          config.NewStore(filepath.Join(home, ".remork")),
		storeReady:     true,
		probe:          probe,
		verifyTimeout:  5 * time.Millisecond,
		verifyInterval: time.Millisecond,
		runner:         &fakeCommandRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "/scratch/project") || !strings.Contains(err.Error(), "is not advertised") {
		t.Fatalf("runDaemonDeploy error = %v, want unadvertised second root", err)
	}
}

func TestRunDaemonDeployVerifyRejectsUnadvertisedRoot(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data/other"}}
	home := t.TempDir()

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		root:           "/data/project",
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       "dist/remorkd-linux-arm64",
		url:            "http://lab.example:17731",
		verify:         true,
		execute:        true,
		yes:            true,
		store:          config.NewStore(filepath.Join(home, ".remork")),
		storeReady:     true,
		probe:          probe,
		verifyTimeout:  5 * time.Millisecond,
		verifyInterval: time.Millisecond,
		runner:         &fakeCommandRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "did not become ready") || !strings.Contains(err.Error(), "is not advertised") {
		t.Fatalf("runDaemonDeploy error = %v, want bounded root advertisement error", err)
	}
}

func TestRunDaemonDeployVerifyRetriesUntilReady(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data"}, statusErrorsBeforeSuccess: 2}
	home := t.TempDir()

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		root:           "/data/project",
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       "dist/remorkd-linux-arm64",
		url:            "http://lab.example:17731",
		verify:         true,
		execute:        true,
		yes:            true,
		store:          config.NewStore(filepath.Join(home, ".remork")),
		storeReady:     true,
		probe:          probe,
		verifyTimeout:  50 * time.Millisecond,
		verifyInterval: time.Millisecond,
		runner:         &fakeCommandRunner{},
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	if probe.statusCalls != 3 {
		t.Fatalf("status calls = %d, want 3", probe.statusCalls)
	}
	if !strings.Contains(out.String(), "daemon status verified") {
		t.Fatalf("output should confirm verification, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployVerifyTimesOutWithFinalError(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data"}, statusErrorsBeforeSuccess: 100}
	home := t.TempDir()

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		root:           "/data/project",
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       "dist/remorkd-linux-arm64",
		url:            "http://lab.example:17731",
		verify:         true,
		execute:        true,
		yes:            true,
		store:          config.NewStore(filepath.Join(home, ".remork")),
		storeReady:     true,
		probe:          probe,
		verifyTimeout:  5 * time.Millisecond,
		verifyInterval: time.Millisecond,
		runner:         &fakeCommandRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "daemon verify did not become ready") || !strings.Contains(err.Error(), "status not ready") {
		t.Fatalf("runDaemonDeploy error = %v, want timeout with final status error", err)
	}
	if probe.statusCalls < 2 {
		t.Fatalf("status calls = %d, want multiple retries", probe.statusCalls)
	}
}

func TestRunDaemonDeployVerifyAllowsUpgradeWithoutRoot(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data/other"}}
	home := t.TempDir()

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:     "upgrade",
		hostName:   "lab",
		sshTarget:  "lab.example",
		addr:       "127.0.0.1:17731",
		remoteBin:  ".local/bin/remorkd",
		localBin:   "dist/remorkd-linux-arm64",
		url:        "http://lab.example:17731",
		verify:     true,
		execute:    true,
		yes:        true,
		store:      config.NewStore(filepath.Join(home, ".remork")),
		storeReady: true,
		probe:      probe,
		runner:     &fakeCommandRunner{},
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
}

func TestRunDaemonDeployPrintsHostAddWhenNotExecuting(t *testing.T) {
	var out bytes.Buffer
	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		url:       "http://lab.example:17731",
		tokenEnv:  "REMORK_TOKEN",
		noProxy:   true,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"'remork' 'host' 'add' 'lab' '--url' 'http://lab.example:17731'", "'--token-env' 'REMORK_TOKEN'", "'--no-proxy'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("plan should contain %q, got:\n%s", want, got)
		}
	}
}

func TestRunDaemonDeployVerifyRequiresHostConfig(t *testing.T) {
	var out bytes.Buffer
	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		verify:    true,
		execute:   true,
		yes:       true,
		runner:    &fakeCommandRunner{},
	})
	if err == nil || !strings.Contains(err.Error(), "--verify requires --url or an existing configured host") {
		t.Fatalf("runDaemonDeploy error = %v, want clear verify error", err)
	}
}

func TestInsecureNoTokenNonLoopbackAddrCases(t *testing.T) {
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
			if got := insecureNoTokenNonLoopbackAddr(tt.addr, tt.hasToken); got != tt.want {
				t.Fatalf("insecureNoTokenNonLoopbackAddr(%q, %t) = %t, want %t", tt.addr, tt.hasToken, got, tt.want)
			}
		})
	}
}

type recordingDaemonProbe struct {
	statusCalls               int
	statusHost                config.Host
	roots                     []string
	statusErrorsBeforeSuccess int
}

func (p *recordingDaemonProbe) Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error) {
	p.statusCalls++
	p.statusHost = host
	if p.statusCalls <= p.statusErrorsBeforeSuccess {
		return api.StatusResponse{}, fmt.Errorf("status not ready")
	}
	return api.StatusResponse{Roots: p.roots}, nil
}

func (p *recordingDaemonProbe) Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error) {
	return api.ManifestResponse{}, nil
}

func (p *recordingDaemonProbe) Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error) {
	return nil, nil
}
