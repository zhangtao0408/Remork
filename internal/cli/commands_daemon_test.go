package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"remork/internal/api"
	"remork/internal/client"
	"remork/internal/config"
	"remork/internal/ops"
	"remork/internal/output"
	"remork/internal/tui"
)

type recordedCommand struct {
	name string
	args []string
}

type fakeCommandRunner struct {
	commands       []recordedCommand
	outputCommands []recordedCommand
	failAt         int
	outputs        [][]byte
	outputErrors   []error
}

func (f *fakeCommandRunner) Run(name string, args ...string) error {
	f.commands = append(f.commands, recordedCommand{name: name, args: append([]string(nil), args...)})
	if f.failAt > 0 && len(f.commands) == f.failAt {
		return fmt.Errorf("command failed")
	}
	return nil
}

func (f *fakeCommandRunner) Output(name string, args ...string) ([]byte, error) {
	f.outputCommands = append(f.outputCommands, recordedCommand{name: name, args: append([]string(nil), args...)})
	idx := len(f.outputCommands) - 1
	var out []byte
	if idx < len(f.outputs) {
		out = f.outputs[idx]
	}
	if idx < len(f.outputErrors) && f.outputErrors[idx] != nil {
		return out, f.outputErrors[idx]
	}
	return out, nil
}

func fakeDaemonBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "remorkd")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'remorkd test\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake daemon binary: %v", err)
	}
	return path
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

func TestDaemonInstallPlanHonorsForcedColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()}), "--color=always", "daemon", "install", "lab", "--root", "/data", "--local-bin", fakeDaemonBinary(t), "--dry-run")
	if err != nil {
		t.Fatalf("daemon install plan: %v", err)
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("--color=always should force ANSI in daemon install plan, got:\n%s", out.String())
	}
}

func TestDaemonDeployDryRunClearlySaysNothingExecuted(t *testing.T) {
	var out bytes.Buffer

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "upgrade",
		hostName:  "lab",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  fakeDaemonBinary(t),
		dryRun:    true,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"mode: dry run preview",
		"No remote commands were executed.",
		"pass -y/--yes to execute",
		"preview generated; remote daemon was not changed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dry-run plan should contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "daemon deploy executed") {
		t.Fatalf("dry-run output should not look like execution success, got:\n%s", got)
	}
}

func TestDaemonDeployModeInteractiveDefaultsToExecute(t *testing.T) {
	deploy := daemonDeployOptions{}

	configureDaemonDeployExecution(&deploy, false)

	if !deploy.execute || deploy.yes || deploy.dryRun {
		t.Fatalf("interactive deploy should default to execute with confirmation, got execute=%t yes=%t dryRun=%t", deploy.execute, deploy.yes, deploy.dryRun)
	}
}

func TestDaemonDeployModeDryRunOverridesInteractiveExecution(t *testing.T) {
	deploy := daemonDeployOptions{execute: true, yes: true}

	configureDaemonDeployExecution(&deploy, true)

	if deploy.execute || deploy.yes || !deploy.dryRun {
		t.Fatalf("--dry-run should force preview mode, got execute=%t yes=%t dryRun=%t", deploy.execute, deploy.yes, deploy.dryRun)
	}
}

func TestDaemonDeployModeExplicitExecuteStillRequiresExplicitYes(t *testing.T) {
	deploy := daemonDeployOptions{execute: true}

	configureDaemonDeployExecution(&deploy, false)

	if !deploy.execute || deploy.yes || deploy.dryRun {
		t.Fatalf("explicit --execute should be preserved and still require --yes, got execute=%t yes=%t dryRun=%t", deploy.execute, deploy.yes, deploy.dryRun)
	}
}

func TestDaemonDeployModeNonInteractiveStillDefaultsToExecute(t *testing.T) {
	deploy := daemonDeployOptions{}

	configureDaemonDeployExecution(&deploy, false)

	if !deploy.execute || deploy.yes || deploy.dryRun {
		t.Fatalf("non-interactive omitted flags should require confirmation instead of dry-run, got execute=%t yes=%t dryRun=%t", deploy.execute, deploy.yes, deploy.dryRun)
	}
}

func TestDaemonDeployCommandWithoutDryRunRequiresConfirmationInPlainMode(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()}), "daemon", "install", "lab", "--root", "/data", "--addr", "127.0.0.1:17731", "--local-bin", fakeDaemonBinary(t))
	if err == nil || !strings.Contains(err.Error(), "requires confirmation") {
		t.Fatalf("daemon install without --dry-run/-y error = %v, output=%q; want confirmation requirement", err, out.String())
	}
	if !strings.Contains(commandErrorFix(err), "--dry-run") || !strings.Contains(commandErrorFix(err), "-y") {
		t.Fatalf("fix = %q, want -y and --dry-run guidance", commandErrorFix(err))
	}
	if strings.Contains(out.String(), "dry run preview") || strings.Contains(out.String(), "No remote commands were executed") {
		t.Fatalf("plain command without --dry-run should not render dry-run output, got:\n%s", out.String())
	}
}

func TestDaemonDeployCommandWithoutYesDoesNotProbeSSH(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join("dist", "remorkd-linux-arm64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write dist binary: %v", err)
	}
	fake := &fakeCommandRunner{outputs: [][]byte{
		[]byte("missing remorkd\n"),
		[]byte("Linux\naarch64\n"),
	}}

	out, err := executeCommand(NewRootCommand(Options{
		Version:       "v1.2.3",
		HomeDir:       t.TempDir(),
		CommandRunner: fake,
	}), "daemon", "install", "lab", "--root", "/data", "--addr", "127.0.0.1:17731", "--non-interactive")
	if err == nil || !strings.Contains(err.Error(), "requires confirmation") {
		t.Fatalf("daemon install without -y error = %v, output=%q; want confirmation requirement", err, out.String())
	}
	if len(fake.commands) != 0 || len(fake.outputCommands) != 0 {
		t.Fatalf("SSH probes ran before confirmation: run=%#v output=%#v", fake.commands, fake.outputCommands)
	}
}

func TestDaemonDeployCommandRejectsUnsafeBindBeforeSSHProbe(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join("dist", "remorkd-linux-arm64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write dist binary: %v", err)
	}
	fake := &fakeCommandRunner{outputs: [][]byte{
		[]byte("missing remorkd\n"),
		[]byte("Linux\naarch64\n"),
	}}

	out, err := executeCommand(NewRootCommand(Options{
		Version:       "v1.2.3",
		HomeDir:       t.TempDir(),
		CommandRunner: fake,
	}), "daemon", "install", "lab", "--root", "/data", "--addr", "0.0.0.0:17731", "-y", "--non-interactive")
	if err == nil || !strings.Contains(err.Error(), "--allow-unauthenticated-network-bind") {
		t.Fatalf("daemon install unsafe bind error = %v, output=%q; want unsafe bind rejection", err, out.String())
	}
	if len(fake.commands) != 0 || len(fake.outputCommands) != 0 {
		t.Fatalf("SSH probes ran before unsafe bind rejection: run=%#v output=%#v", fake.commands, fake.outputCommands)
	}
}

func TestDaemonDeployCommandYesAliasExecutes(t *testing.T) {
	fake := &fakeCommandRunner{}
	out, err := executeCommand(NewRootCommand(Options{HomeDir: t.TempDir(), CommandRunner: fake}), "daemon", "install", "lab", "--root", "/data", "--addr", "127.0.0.1:17731", "--local-bin", fakeDaemonBinary(t), "-y")
	if err != nil {
		t.Fatalf("daemon install -y returned error: %v\n%s", err, out.String())
	}
	if len(fake.commands) == 0 {
		t.Fatalf("daemon install -y should execute remote commands, output:\n%s", out.String())
	}
	if strings.Contains(out.String(), "dry run preview") {
		t.Fatalf("daemon install -y should not print dry-run mode, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "daemon deploy executed") {
		t.Fatalf("daemon install -y should confirm execution, got:\n%s", out.String())
	}
}

func TestDaemonDeployFormIncludesExecutionAndSafetyFields(t *testing.T) {
	fields := daemonDeployFormFields("upgrade", daemonDeployOptions{
		hostName:                        "lab",
		roots:                           []string{"/home/me"},
		addr:                            "0.0.0.0:17731",
		remoteBin:                       ".local/bin/remorkd",
		allowUnauthenticatedNetworkBind: true,
	}, false)

	got := map[string]tui.Field{}
	for _, field := range fields {
		got[field.Key] = field
	}
	for _, key := range []string{
		"host",
		"roots",
		"ssh",
		"url",
		"addr",
		"remote_bin",
		"local_bin",
		"platform",
		"token_file",
		"token_env",
		"verify",
		"no_proxy",
		"allow_unauthenticated_network_bind",
		"dry_run",
		"yes",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("daemon form missing field %q; got %#v", key, got)
		}
	}
	if got["host"].Initial != "lab" || got["roots"].Initial != "/home/me" {
		t.Fatalf("form should preserve host/root initial values, got host=%q roots=%q", got["host"].Initial, got["roots"].Initial)
	}
	if got["allow_unauthenticated_network_bind"].Initial != "yes" {
		t.Fatalf("allow unauth bind should be visible and preserved, got %q", got["allow_unauthenticated_network_bind"].Initial)
	}
}

func TestApplyDaemonDeployFormValuesParsesSafetyFields(t *testing.T) {
	deploy := daemonDeployOptions{addr: "0.0.0.0:17731", remoteBin: ".local/bin/remorkd"}
	dryRun, err := applyDaemonDeployFormValues(&deploy, map[string]string{
		"host":                               "lab",
		"roots":                              "/home/me, /scratch",
		"ssh":                                "user@server",
		"url":                                "http://server:17731",
		"addr":                               "0.0.0.0:17731",
		"remote_bin":                         ".local/bin/remorkd",
		"local_bin":                          "/tmp/remorkd",
		"platform":                           "linux-arm64",
		"token_file":                         ".remork/token",
		"token_env":                          "REMORK_TOKEN",
		"verify":                             "yes",
		"no_proxy":                           "true",
		"allow_unauthenticated_network_bind": "y",
		"dry_run":                            "no",
		"yes":                                "n",
	})
	if err != nil {
		t.Fatalf("applyDaemonDeployFormValues returned error: %v", err)
	}
	if dryRun {
		t.Fatal("dry-run should be false")
	}
	if deploy.hostName != "lab" || deploy.sshTarget != "user@server" || deploy.url != "http://server:17731" {
		t.Fatalf("deploy target fields not parsed: %#v", deploy)
	}
	if strings.Join(deployAllowedRoots(deploy), ",") != "/home/me,/scratch" {
		t.Fatalf("roots = %#v", deployAllowedRoots(deploy))
	}
	if !deploy.verify || !deploy.noProxy || !deploy.allowUnauthenticatedNetworkBind || deploy.yes {
		t.Fatalf("boolean fields not parsed: verify=%t noProxy=%t allow=%t yes=%t", deploy.verify, deploy.noProxy, deploy.allowUnauthenticatedNetworkBind, deploy.yes)
	}
}

func TestDaemonDeployFormValuesRejectInvalidBoolean(t *testing.T) {
	deploy := daemonDeployOptions{}
	_, err := applyDaemonDeployFormValues(&deploy, map[string]string{
		"host":                               "lab",
		"roots":                              "/home/me",
		"allow_unauthenticated_network_bind": "maybe",
	})
	if err == nil || !strings.Contains(err.Error(), "allow unauthenticated network bind") {
		t.Fatalf("error = %v, want invalid boolean guidance", err)
	}
}

func TestRunDaemonDeployExecuteRunsCommandsInOrder(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  localBin,
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}

	want := []recordedCommand{
		{name: "ssh", args: []string{"lab.example", remotePrepareCommand(daemonDeployOptions{remoteBin: ".local/bin/remorkd"})}},
		{name: "ssh", args: []string{"lab.example", remoteStopCommand()}},
		{name: "scp", args: []string{localBin, "lab.example:.local/bin/remorkd"}},
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
	if !strings.Contains(out.String(), "== Daemon install ==") {
		t.Fatalf("output should include styled daemon section, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "prepare remote directories...\n") {
		t.Fatalf("deploy steps should rewrite the running line instead of leaving a separate step line, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "\r") {
		t.Fatalf("deploy steps should use carriage returns for live output, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployValidatesLocalBinBeforeRemoteMutation(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  filepath.Join(t.TempDir(), "missing-remorkd"),
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err == nil || !strings.Contains(err.Error(), "local remorkd binary") {
		t.Fatalf("runDaemonDeploy error = %v, want local binary preflight", err)
	}
	if len(fake.commands) != 0 || len(fake.outputCommands) != 0 {
		t.Fatalf("remote commands ran despite local preflight failure: run=%#v output=%#v", fake.commands, fake.outputCommands)
	}
	if out.String() != "" {
		t.Fatalf("preflight should fail before printing plan, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployAllowsReadableLocalBinWithoutExecBit(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}
	localBin := filepath.Join(t.TempDir(), "remorkd-linux-arm64")
	if err := os.WriteFile(localBin, []byte("daemon"), 0o644); err != nil {
		t.Fatalf("write local bin: %v", err)
	}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  localBin,
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy should not require local executable bit: %v", err)
	}
	if len(fake.commands) == 0 {
		t.Fatalf("deploy should run remote commands")
	}
}

func TestRunDaemonDeploySkipsCopyWhenRemoteVersionMatches(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{outputs: [][]byte{[]byte("installed\tremorkd v1.2.3\n")}}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:            "upgrade",
		hostName:          "lab",
		sshTarget:         "lab.example",
		root:              "/data/project",
		addr:              "127.0.0.1:17731",
		remoteBin:         ".local/bin/remorkd",
		execute:           true,
		yes:               true,
		version:           "v1.2.3",
		skipBinaryInstall: true,
		runner:            fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	for _, cmd := range fake.commands {
		if cmd.name == "scp" {
			t.Fatalf("compatible remote remorkd should skip scp, commands=%#v", fake.commands)
		}
		if cmd.name == "ssh" && len(cmd.args) > 1 && strings.Contains(cmd.args[1], "chmod 0755") {
			t.Fatalf("compatible remote remorkd should skip chmod for copied binary, commands=%#v", fake.commands)
		}
	}
	if !strings.Contains(out.String(), "using existing compatible remorkd") {
		t.Fatalf("output should explain skipped binary install, got:\n%s", out.String())
	}
}

func TestRunDaemonDeploySkipsCopyWhenRemoteDevVersionMatches(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{outputs: [][]byte{[]byte("installed\tremorkd dev\n")}}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:            "upgrade",
		hostName:          "lab",
		sshTarget:         "lab.example",
		root:              "/data/project",
		addr:              "127.0.0.1:17731",
		remoteBin:         ".local/bin/remorkd",
		execute:           true,
		yes:               true,
		version:           "dev",
		skipBinaryInstall: true,
		runner:            fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	for _, cmd := range fake.commands {
		if cmd.name == "scp" {
			t.Fatalf("matching dev remote remorkd should skip scp, commands=%#v", fake.commands)
		}
	}
}

func TestRemoteBinaryAlreadyCompatibleTreatsDevAsComparable(t *testing.T) {
	fake := &fakeCommandRunner{outputs: [][]byte{[]byte("installed\tremorkd dev\n")}}
	ok := remoteBinaryAlreadyCompatible(fake, "lab.example", daemonDeployOptions{remoteBin: ".local/bin/remorkd"}, "dev")
	if !ok {
		t.Fatal("remote dev remorkd should be compatible with local dev client")
	}
}

func TestDaemonInstallRejectsRelativeAllowedRoot(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()}), "daemon", "install", "lab", "--root", "relative/path", "--local-bin", "dist/remorkd-linux-arm64")
	if err == nil {
		t.Fatal("daemon install should reject relative allowed roots")
	}
	if !strings.Contains(err.Error(), "absolute") || !strings.Contains(err.Error(), "--root") {
		t.Fatalf("error = %v, want absolute root guidance", err)
	}
	if out.String() != "" {
		t.Fatalf("relative root should reject before plan output, got:\n%s", out.String())
	}
}

func TestDaemonInstallWithoutHostInPlainModeReturnsHelpfulError(t *testing.T) {
	cmd := NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()})

	_, err := executeCommand(cmd, "daemon", "install", "--non-interactive")
	if err == nil {
		t.Fatal("daemon install without host should fail in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "requires HOST") {
		t.Fatalf("error should explain missing host, got %v", err)
	}
}

func TestDetectRemoteDaemonPlatformMapsLinuxArchitectures(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{name: "arm64", out: "Linux\naarch64\n", want: "linux-arm64"},
		{name: "x86_64", out: "linux\nx86_64\n", want: "linux-amd64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCommandRunner{outputs: [][]byte{[]byte(tt.out)}}
			got, err := detectRemoteDaemonPlatform(context.Background(), fake, "lab.example")
			if err != nil {
				t.Fatalf("detectRemoteDaemonPlatform returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("platform = %q, want %q", got, tt.want)
			}
			if len(fake.outputCommands) != 1 || fake.outputCommands[0].name != "ssh" || fake.outputCommands[0].args[0] != "lab.example" {
				t.Fatalf("platform detection should use ssh target, got %#v", fake.outputCommands)
			}
		})
	}
}

func TestDaemonVendorPlatformItemsIncludesPackagedBinaries(t *testing.T) {
	vendorDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vendorDir, "remorkd-linux-arm64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write arm vendor binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "remorkd-linux-amd64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write amd vendor binary: %v", err)
	}

	items := daemonVendorPlatformItems(vendorDir)
	if len(items) != 2 {
		t.Fatalf("items = %#v, want two packaged daemon platforms", items)
	}
	if items[0].Name != "linux-arm64" || strings.Join(items[0].Args, " ") != "linux-arm64" {
		t.Fatalf("first item = %#v, want linux-arm64", items[0])
	}
	if items[1].Name != "linux-amd64" || strings.Join(items[1].Args, " ") != "linux-amd64" {
		t.Fatalf("second item = %#v, want linux-amd64", items[1])
	}
}

func TestChooseDaemonVendorPlatformRejectsMissingVendorBinaries(t *testing.T) {
	var in bytes.Buffer
	var out bytes.Buffer
	_, err := chooseDaemonVendorPlatform(&in, &out, output.ColorNever, t.TempDir())
	if err == nil {
		t.Fatal("chooseDaemonVendorPlatform returned nil error, want missing vendor binary error")
	}
	if !strings.Contains(err.Error(), "vendor remorkd binaries are not available") {
		t.Fatalf("error = %q, want missing vendor guidance", err.Error())
	}
}

func TestDaemonInstallAutoDetectsRemotePlatformForReleaseBinary(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join("dist", "remorkd-linux-arm64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write dist binary: %v", err)
	}
	fake := &fakeCommandRunner{outputs: [][]byte{[]byte("Linux\naarch64\n")}}

	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{
		Version:       "v1.2.3",
		HomeDir:       t.TempDir(),
		CommandRunner: fake,
	}), "daemon", "install", "lab", "--root", "/data", "--dry-run", "--non-interactive")
	if err != nil {
		t.Fatalf("daemon install should auto-detect remote platform: %v", err)
	}
	if !strings.Contains(stdout.String(), "dist/remorkd-linux-arm64") {
		t.Fatalf("install plan should use detected linux-arm64 binary, got:\n%s", stdout.String())
	}
	for _, want := range []string{"detecting remote platform over SSH", "detected remote platform: linux-arm64"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("platform detection progress should contain %q, got:\n%s", want, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "detecting remote platform over SSH...\n") {
		t.Fatalf("platform detection should rewrite the running line instead of leaving a separate step line, got:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "\r") {
		t.Fatalf("platform detection should use carriage returns for live output, got:\n%s", stderr.String())
	}
	if len(fake.outputCommands) != 1 || fake.outputCommands[0].name != "ssh" || fake.outputCommands[0].args[0] != "lab" {
		t.Fatalf("platform detection should probe host with ssh, got %#v", fake.outputCommands)
	}
}

func TestDaemonInstallSkipsReleaseResolveWhenRemoteVersionMatches(t *testing.T) {
	fake := &fakeCommandRunner{outputs: [][]byte{
		[]byte("installed\tremorkd v1.2.3\n"),
		[]byte("installed\tremorkd v1.2.3\n"),
	}}

	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{
		Version:       "v1.2.3",
		HomeDir:       t.TempDir(),
		CommandRunner: fake,
	}), "daemon", "install", "lab", "--root", "/data", "--addr", "127.0.0.1:17731", "-y", "--non-interactive")
	if err != nil {
		t.Fatalf("daemon install should reuse compatible remote remorkd: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	for _, cmd := range fake.outputCommands {
		if cmd.name == "ssh" && len(cmd.args) > 1 && strings.Contains(cmd.args[1], "uname") {
			t.Fatalf("compatible remote remorkd should skip platform detection and release resolve, output commands=%#v", fake.outputCommands)
		}
	}
	for _, cmd := range fake.commands {
		if cmd.name == "scp" {
			t.Fatalf("compatible remote remorkd should skip copying release binary, commands=%#v", fake.commands)
		}
	}
	if !strings.Contains(stdout.String(), "using existing compatible remorkd") {
		t.Fatalf("stdout should explain reused binary, got:\n%s", stdout.String())
	}
}

func TestDaemonStatusFreshConfigReturnsHelpfulError(t *testing.T) {
	out, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()}), "daemon", "status", "lab")
	if err == nil {
		t.Fatal("daemon status without config should fail")
	}
	if strings.Contains(err.Error(), "config.json") || strings.Contains(out.String(), "config.json") {
		t.Fatalf("daemon status leaked raw config path: err=%v out=%q", err, out.String())
	}
	if !strings.Contains(err.Error(), "remork is not configured") || !strings.Contains(err.Error(), "remork host add lab --url URL") {
		t.Fatalf("daemon status error = %v, want first-run host add guidance", err)
	}
}

func TestDaemonStatusJSONFreshConfigReturnsStructuredError(t *testing.T) {
	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()}), "daemon", "status", "lab", "--json")
	if err == nil {
		t.Fatal("daemon status --json without config should fail")
	}
	if stderr.String() != "" {
		t.Fatalf("json error should not write stderr, got %q", stderr.String())
	}
	var got commandErrorJSON
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatalf("output is not strict JSON: %q: %v", stdout.String(), jsonErr)
	}
	if !strings.Contains(got.Error, "remork is not configured") || !strings.Contains(got.Fix, "remork host add lab --url URL") || got.Code == 0 {
		t.Fatalf("json error = %#v", got)
	}
}

func TestDaemonStatusJSONSuccess(t *testing.T) {
	home := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(api.StatusResponse{
			Version:        "v-test",
			Platform:       "linux/arm64",
			Roots:          []string{"/data", "/scratch"},
			Threshold:      134217728,
			WatchSupported: true,
		})
	}))
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{Hosts: map[string]config.Host{
		"lab": {Name: "lab", URL: server.URL, NoProxy: true},
	}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout, stderr, err := executeCommandSplit(NewRootCommand(Options{Version: "test", HomeDir: home}), "daemon", "status", "lab", "--json")
	if err != nil {
		t.Fatalf("daemon status --json: %v", err)
	}
	if stderr.String() != "" {
		t.Fatalf("daemon status --json should not write stderr, got %q", stderr.String())
	}
	var got struct {
		Host               string   `json:"host"`
		URL                string   `json:"url"`
		Reachable          bool     `json:"reachable"`
		Version            string   `json:"version"`
		Platform           string   `json:"platform"`
		AllowedRoots       []string `json:"allowed_roots"`
		Auth               string   `json:"auth"`
		LargeFileThreshold int64    `json:"large_file_threshold"`
		WatchSupported     bool     `json:"watch_supported"`
	}
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatalf("output is not strict JSON: %q: %v", stdout.String(), jsonErr)
	}
	if got.Host != "lab" || got.URL != server.URL || !got.Reachable || got.Version != "v-test" || got.Platform != "linux/arm64" || got.LargeFileThreshold != 134217728 || !got.WatchSupported {
		t.Fatalf("daemon status json = %#v", got)
	}
	if strings.Join(got.AllowedRoots, ",") != "/data,/scratch" {
		t.Fatalf("allowed roots = %#v", got.AllowedRoots)
	}
	if got.Auth == "" {
		t.Fatalf("auth state should be populated: %#v", got)
	}
}

func TestDaemonStatusMapsConnectionErrorsToActionableMessage(t *testing.T) {
	home := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: "http://127.0.0.1:1"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home}), "daemon", "status", "lab")
	if err == nil {
		t.Fatal("daemon status returned nil error")
	}
	if strings.Contains(err.Error(), "Get ") || strings.Contains(err.Error(), "/status") {
		t.Fatalf("daemon status leaked raw HTTP error: %v", err)
	}
	mustContain(t, err.Error(), "connection refused")
	var coded interface{ Fix() string }
	if !errors.As(err, &coded) || !strings.Contains(coded.Fix(), "start remorkd") {
		t.Fatalf("daemon status fix = %q, want start guidance", commandErrorFix(err))
	}
}

func TestDaemonStatusMapsAuthErrorsToActionableMessage(t *testing.T) {
	home := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: server.URL, TokenEnv: "REMORK_TOKEN"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("REMORK_TOKEN", "wrong")

	_, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home}), "daemon", "status", "lab")
	if err == nil {
		t.Fatal("daemon status returned nil error")
	}
	mustContain(t, err.Error(), "auth failed")
	if !strings.Contains(commandErrorFix(err), "export REMORK_TOKEN") {
		t.Fatalf("fix = %q, want token env guidance", commandErrorFix(err))
	}
}

func TestDaemonStatusMissingTokenEnvHasFixAndPermissionCode(t *testing.T) {
	home := t.TempDir()
	store := config.NewStore(filepath.Join(home, ".remork"))
	if err := store.Save(config.Config{
		Hosts: map[string]config.Host{
			"lab": {Name: "lab", URL: "http://127.0.0.1:17731", TokenEnv: "REMORK_TOKEN"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: home}), "daemon", "status", "lab")
	if err == nil {
		t.Fatal("daemon status returned nil error")
	}
	mustContain(t, err.Error(), "REMORK_TOKEN")
	if code := commandErrorExitCode(err); code != 6 {
		t.Fatalf("exit code = %d, want permission code 6", code)
	}
	if fix := commandErrorFix(err); !strings.Contains(fix, "export REMORK_TOKEN") {
		t.Fatalf("fix = %q, want token export guidance", fix)
	}
}

func TestRunDaemonDeployReportsRemoteBinaryStateAndChecksCopiedVersion(t *testing.T) {
	var out bytes.Buffer
	localBin := fakeDaemonBinary(t)
	fake := &fakeCommandRunner{
		outputs: [][]byte{
			[]byte("remorkd v0.1.0\n"),
			[]byte("remorkd v0.1.1.beta01\n"),
		},
	}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  localBin,
		version:   "v0.1.1.beta01",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	if len(fake.outputCommands) != 2 {
		t.Fatalf("version checks = %d, want pre and post copy checks: %#v", len(fake.outputCommands), fake.outputCommands)
	}
	for _, want := range []string{
		"remote binary: installed remorkd v0.1.0",
		"will replace with v0.1.1.beta01",
		"remote binary: installed remorkd v0.1.1.beta01",
		"copied remorkd version verified: v0.1.1.beta01",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output should contain %q, got:\n%s", want, out.String())
		}
	}
}

func TestRunDaemonDeployRejectsCopiedVersionMismatch(t *testing.T) {
	var out bytes.Buffer
	localBin := fakeDaemonBinary(t)
	fake := &fakeCommandRunner{
		outputs: [][]byte{
			[]byte("remorkd v0.1.0\n"),
			[]byte("remorkd v0.1.0\n"),
		},
	}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  localBin,
		version:   "v0.1.1.beta01",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err == nil || !strings.Contains(err.Error(), "remote remorkd version mismatch after copy") || !strings.Contains(err.Error(), "want v0.1.1.beta01") {
		t.Fatalf("runDaemonDeploy error = %v, want copied version mismatch", err)
	}
	if len(fake.commands) != 4 {
		t.Fatalf("commands after mismatch = %d, want stop before start: %#v", len(fake.commands), fake.commands)
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
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: "/tmp/remork d;touch x",
		localBin:  localBin,
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

	chmod := fake.commands[3]
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

func TestRunDaemonDeployUpgradeRequiresRootsBeforeRemoteMutation(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "upgrade",
		hostName:  "lab",
		sshTarget: "lab.example",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  localBin,
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err == nil || !strings.Contains(err.Error(), "daemon upgrade requires at least one --root") {
		t.Fatalf("runDaemonDeploy error = %v, want missing upgrade root preflight", err)
	}
	if len(fake.commands) != 0 || len(fake.outputCommands) != 0 {
		t.Fatalf("remote commands ran despite missing roots: run=%#v output=%#v", fake.commands, fake.outputCommands)
	}
	if out.String() != "" {
		t.Fatalf("preflight should fail before printing plan, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployUpgradeWithRootsRestartsDaemon(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "upgrade",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  localBin,
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	if len(fake.commands) != 5 {
		t.Fatalf("ran %d commands, want prepare/stop/copy/chmod/start: %#v", len(fake.commands), fake.commands)
	}
	start := fake.commands[4]
	if start.name != "ssh" || !strings.Contains(strings.Join(start.args, " "), "--root") || !strings.Contains(strings.Join(start.args, " "), "/data/project") {
		t.Fatalf("upgrade with roots should restart daemon with roots, got %#v", start)
	}
}

func TestRunDaemonDeployRejectsInvalidAddrBeforePlanOrRemoteMutation(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		sshTarget: "lab.example",
		root:      "/data/project",
		addr:      "bad",
		remoteBin: ".local/bin/remorkd",
		localBin:  fakeDaemonBinary(t),
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid daemon listen address") {
		t.Fatalf("runDaemonDeploy error = %v, want invalid addr preflight", err)
	}
	if len(fake.commands) != 0 || len(fake.outputCommands) != 0 {
		t.Fatalf("remote commands ran despite invalid addr: run=%#v output=%#v", fake.commands, fake.outputCommands)
	}
	if out.String() != "" {
		t.Fatalf("invalid addr should fail before plan output, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployRejectsInvalidURLBeforePlanOrConfigWrite(t *testing.T) {
	var out bytes.Buffer

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  fakeDaemonBinary(t),
		url:       "ftp://example",
	})
	if err == nil || !strings.Contains(err.Error(), "daemon URL must include http:// or https://") {
		t.Fatalf("runDaemonDeploy error = %v, want invalid URL preflight", err)
	}
	if out.String() != "" {
		t.Fatalf("invalid URL should fail before plan output, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployDryRunValidatesLocalBinBeforePlan(t *testing.T) {
	var out bytes.Buffer

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  filepath.Join(t.TempDir(), "missing-remorkd"),
	})
	if err == nil || !strings.Contains(err.Error(), "local remorkd binary") {
		t.Fatalf("runDaemonDeploy error = %v, want local binary dry-run preflight", err)
	}
	if out.String() != "" {
		t.Fatalf("missing local binary should fail before plan output, got:\n%s", out.String())
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
		localBin:  fakeDaemonBinary(t),
		execute:   true,
		runner:    fake,
	})
	if err == nil || !strings.Contains(err.Error(), "requires confirmation") {
		t.Fatalf("runDaemonDeploy error = %v, want confirmation requirement", err)
	}
	if code := commandErrorExitCode(err); code != 7 {
		t.Fatalf("exit code = %d, want prompt-required code 7", code)
	}
	if fix := commandErrorFix(err); !strings.Contains(fix, "-y") || !strings.Contains(fix, "--dry-run") {
		t.Fatalf("fix = %q, want -y and --dry-run guidance", fix)
	}
	if len(fake.commands) != 0 {
		t.Fatalf("ran commands despite missing confirmation: %#v", fake.commands)
	}
	if out.String() != "" {
		t.Fatalf("non-interactive confirmation error should not render a dry-run plan, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployPromptsAndCancelsExecution(t *testing.T) {
	var out bytes.Buffer
	var promptOut bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:     "install",
		hostName:   "lab",
		root:       "/data/project",
		addr:       "127.0.0.1:17731",
		remoteBin:  ".local/bin/remorkd",
		localBin:   fakeDaemonBinary(t),
		execute:    true,
		canPrompt:  true,
		confirmIn:  strings.NewReader("n\n"),
		confirmOut: &promptOut,
		runner:     fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	if len(fake.commands) != 0 {
		t.Fatalf("ran commands despite cancelled confirmation: %#v", fake.commands)
	}
	if !strings.Contains(promptOut.String(), "execute daemon install on lab?") {
		t.Fatalf("prompt output = %q, want confirmation question", promptOut.String())
	}
	if !strings.Contains(out.String(), "mode: execute after confirmation") || !strings.Contains(out.String(), "daemon install cancelled") {
		t.Fatalf("cancelled deploy should show execution mode and cancellation, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployRejectsUnauthenticatedNetworkBindWithoutExplicitAllow(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "0.0.0.0:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		execute:   true,
		yes:       true,
		runner:    fake,
	})
	if err == nil || !strings.Contains(err.Error(), "--allow-unauthenticated-network-bind") {
		t.Fatalf("runDaemonDeploy error = %v, want explicit unsafe bind approval", err)
	}
	if coded, ok := err.(interface{ ExitCode() int }); !ok || coded.ExitCode() != 2 {
		t.Fatalf("exit code = %v, want 2", err)
	}
	if out.String() != "" {
		t.Fatalf("deploy should reject before printing a plan, got:\n%s", out.String())
	}
	if len(fake.commands) != 0 {
		t.Fatalf("ran commands despite unsafe bind rejection: %#v", fake.commands)
	}
}

func TestRunDaemonDeployAllowsExplicitUnauthenticatedNetworkBind(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:                          "install",
		hostName:                        "lab",
		root:                            "/data/project",
		addr:                            "0.0.0.0:17731",
		remoteBin:                       ".local/bin/remorkd",
		localBin:                        localBin,
		execute:                         true,
		yes:                             true,
		allowUnauthenticatedNetworkBind: true,
		runner:                          fake,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	if len(fake.commands) == 0 {
		t.Fatal("expected deploy commands after explicit unsafe bind approval")
	}
}

func TestRunDaemonDeployRequiresTokenEnvWhenTokenFileConfiguresAndVerifiesHost(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{}

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:     "install",
		hostName:   "lab",
		root:       "/data/project",
		addr:       "127.0.0.1:17731",
		remoteBin:  ".local/bin/remorkd",
		localBin:   "dist/remorkd-linux-arm64",
		tokenFile:  "$HOME/.remork/token",
		url:        "http://127.0.0.1:17731",
		verify:     true,
		execute:    true,
		yes:        true,
		store:      config.NewStore(filepath.Join(t.TempDir(), ".remork")),
		storeReady: true,
		runner:     fake,
	})
	if err == nil || !strings.Contains(err.Error(), "--token-env") {
		t.Fatalf("runDaemonDeploy error = %v, want --token-env requirement", err)
	}
	if coded, ok := err.(interface{ ExitCode() int }); !ok || coded.ExitCode() != 2 {
		t.Fatalf("exit code = %v, want 2", err)
	}
	if len(fake.commands) != 0 {
		t.Fatalf("ran commands despite token env preflight failure: %#v", fake.commands)
	}
}

func TestRunDaemonDeployDryRunRequiresTokenEnvWhenTokenFileConfiguresHost(t *testing.T) {
	var out bytes.Buffer

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		tokenFile: "$HOME/.remork/token",
		url:       "http://127.0.0.1:17731",
	})
	if err == nil || !strings.Contains(err.Error(), "--token-env") {
		t.Fatalf("runDaemonDeploy error = %v, want --token-env requirement", err)
	}
	if out.String() != "" {
		t.Fatalf("dry-run should reject before printing bad host add command, got:\n%s", out.String())
	}
}

func TestRunDaemonDeployDryRunRequiresTokenEnvWhenTokenFileOmitsURL(t *testing.T) {
	var out bytes.Buffer

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  "dist/remorkd-linux-arm64",
		tokenFile: "$HOME/.remork/token",
	})
	if err == nil || !strings.Contains(err.Error(), "--token-env") {
		t.Fatalf("runDaemonDeploy error = %v, want --token-env requirement", err)
	}
	if out.String() != "" {
		t.Fatalf("dry-run should reject before printing bad host add guidance, got:\n%s", out.String())
	}
}

func TestDaemonDeployPlanWithoutURLPreservesTokenEnvAndNoProxy(t *testing.T) {
	var out bytes.Buffer

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  fakeDaemonBinary(t),
		tokenFile: "$HOME/.remork/token",
		tokenEnv:  "REMORK_TOKEN",
		noProxy:   true,
	})
	if err != nil {
		t.Fatalf("runDaemonDeploy returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"'remork' 'host' 'add' 'lab' '--url' 'http://HOST:17731'", "'--token-env' 'REMORK_TOKEN'", "'--no-proxy'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("deploy plan missing %q:\n%s", want, got)
		}
	}
}

func TestDaemonDeployVerifyWithoutURLReturnsUsageCode(t *testing.T) {
	_, err := executeCommand(NewRootCommand(Options{Version: "test", HomeDir: t.TempDir()}), "daemon", "install", "lab", "--root", "/data/project", "--verify", "--platform", "linux-arm64", "--non-interactive")
	if err == nil || !strings.Contains(err.Error(), "--verify requires --url") {
		t.Fatalf("daemon install verify error = %v, want --verify requires --url", err)
	}
	if coded, ok := err.(interface{ ExitCode() int }); !ok || coded.ExitCode() != 2 {
		t.Fatalf("exit code = %v, want 2", err)
	}
}

func TestRunDaemonDeployStopsAfterCommandFailure(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeCommandRunner{failAt: 2}
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:    "install",
		hostName:  "lab",
		root:      "/data/project",
		addr:      "127.0.0.1:17731",
		remoteBin: ".local/bin/remorkd",
		localBin:  localBin,
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
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:     "install",
		hostName:   "lab",
		sshTarget:  "lab.example",
		root:       "/data/project",
		addr:       "127.0.0.1:17731",
		remoteBin:  ".local/bin/remorkd",
		localBin:   localBin,
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
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		roots:          []string{"/data/project", "/scratch/project"},
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       localBin,
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
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		roots:          []string{"/data/project", "/scratch/project"},
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       localBin,
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
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		root:           "/data/project",
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       localBin,
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

func TestRunDaemonDeployVerifyRejectsDaemonVersionMismatch(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data"}, version: "v0.1.0"}
	home := t.TempDir()
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		root:           "/data/project",
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       localBin,
		url:            "http://lab.example:17731",
		verify:         true,
		execute:        true,
		yes:            true,
		store:          config.NewStore(filepath.Join(home, ".remork")),
		storeReady:     true,
		probe:          probe,
		verifyTimeout:  5 * time.Millisecond,
		verifyInterval: time.Millisecond,
		runner:         &fakeCommandRunner{outputs: [][]byte{[]byte("remorkd v0.1.1.beta01\n"), []byte("remorkd v0.1.1.beta01\n")}},
		version:        "v0.1.1.beta01",
	})
	if err == nil || !strings.Contains(err.Error(), "daemon version mismatch") || !strings.Contains(err.Error(), "got v0.1.0") || !strings.Contains(err.Error(), "want v0.1.1.beta01") {
		t.Fatalf("runDaemonDeploy error = %v, want daemon version mismatch", err)
	}
}

func TestExplainDaemonStatusErrorGivesActionableReasons(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "auth", err: &client.HTTPError{StatusCode: 401, Body: "unauthorized"}, want: "auth failed"},
		{name: "not remorkd endpoint", err: &client.HTTPError{StatusCode: 404, Body: "not found"}, want: "/status was not found"},
		{name: "connection refused", err: fmt.Errorf("dial tcp 127.0.0.1:17731: connect: connection refused"), want: "not listening"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := explainDaemonStatusError(tt.err); !strings.Contains(got, tt.want) {
				t.Fatalf("explainDaemonStatusError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunDaemonDeployVerifyRetriesUntilReady(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data"}, statusErrorsBeforeSuccess: 2}
	home := t.TempDir()
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		root:           "/data/project",
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       localBin,
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
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:         "install",
		hostName:       "lab",
		sshTarget:      "lab.example",
		root:           "/data/project",
		addr:           "127.0.0.1:17731",
		remoteBin:      ".local/bin/remorkd",
		localBin:       localBin,
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

func TestRunDaemonDeployVerifyAllowsUpgradeWithRoot(t *testing.T) {
	var out bytes.Buffer
	probe := &recordingDaemonProbe{roots: []string{"/data"}}
	home := t.TempDir()
	localBin := fakeDaemonBinary(t)

	err := runDaemonDeploy(&out, daemonDeployOptions{
		action:     "upgrade",
		hostName:   "lab",
		sshTarget:  "lab.example",
		root:       "/data/project",
		addr:       "127.0.0.1:17731",
		remoteBin:  ".local/bin/remorkd",
		localBin:   localBin,
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
		localBin:  fakeDaemonBinary(t),
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
	version                   string
	statusErrorsBeforeSuccess int
}

func (p *recordingDaemonProbe) Status(ctx context.Context, host config.Host, clientID string) (api.StatusResponse, error) {
	p.statusCalls++
	p.statusHost = host
	if p.statusCalls <= p.statusErrorsBeforeSuccess {
		return api.StatusResponse{}, fmt.Errorf("status not ready")
	}
	return api.StatusResponse{Roots: p.roots, Version: p.version}, nil
}

func (p *recordingDaemonProbe) Manifest(ctx context.Context, host config.Host, cfg config.Config, root string) (api.ManifestResponse, error) {
	return api.ManifestResponse{}, nil
}

func (p *recordingDaemonProbe) Operations(ctx context.Context, host config.Host, cfg config.Config, root string, limit int) ([]ops.Entry, error) {
	return nil, nil
}
