package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
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
		remoteBin: "/tmp/remorkd",
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
		remoteBin: "/tmp/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}

	want := []recordedCommand{
		{name: "scp", args: []string{"dist/remorkd-linux-arm64", "lab.example:/tmp/remorkd"}},
		{name: "ssh", args: []string{"lab.example", remoteChmodCommand("/tmp/remorkd")}},
		{name: "ssh", args: []string{"lab.example", remoteStartCommand(daemonDeployOptions{
			root:      "/data/project",
			addr:      "127.0.0.1:17731",
			remoteBin: "/tmp/remorkd",
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

func TestRunDaemonDeployQuotesRemoteChmodPath(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
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
	if len(fake.commands) < 2 {
		t.Fatalf("ran %d commands, want chmod command: %#v", len(fake.commands), fake.commands)
	}

	chmod := fake.commands[1]
	want := "chmod 0755 '/tmp/remork d;touch x'"
	if chmod.name != "ssh" || len(chmod.args) != 2 || chmod.args[1] != want {
		t.Fatalf("chmod command = %#v, want ssh lab.example %q", chmod, want)
	}
	if strings.Contains(chmod.args[1], "chmod 0755 /tmp/remork d;touch x") {
		t.Fatalf("chmod command contains unquoted remote path injection: %q", chmod.args[1])
	}
	if !strings.Contains(out.String(), shellQuote(want)) {
		t.Fatalf("deploy plan should print quoted chmod command %q, got:\n%s", shellQuote(want), out.String())
	}
	if strings.Contains(out.String(), shellQuote("chmod 0755 /tmp/remork d;touch x")) {
		t.Fatalf("deploy plan contains unquoted remote path injection:\n%s", out.String())
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
		remoteBin: "/tmp/remorkd",
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
		remoteBin: "/tmp/remorkd",
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
