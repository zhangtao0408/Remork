package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

type commandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, externalCommandArgs(name, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (osCommandRunner) Output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, externalCommandArgs(name, args...)...)
	cmd.Stdin = os.Stdin
	if name != "ssh" {
		return cmd.CombinedOutput()
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return stdout.Bytes(), err
}

func externalCommandArgs(name string, args ...string) []string {
	switch name {
	case "ssh":
		if len(args) >= 2 {
			return sshCommandArgs(args[0], args[1])
		}
	case "scp":
		if len(args) >= 2 {
			return scpCommandArgs(args[0], args[1])
		}
	}
	return args
}

func sshCommandArgs(remote, command string) []string {
	args := append([]string{}, sshCommonOptions()...)
	args = append(args, remote, command)
	return args
}

func scpCommandArgs(local, remote string) []string {
	args := append([]string{}, sshCommonOptions()...)
	args = append(args, local, remote)
	return args
}

func sshCommonOptions() []string {
	opts := []string{
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=2",
		"-o", "NumberOfPasswordPrompts=3",
	}
	if runtime.GOOS == "windows" {
		return opts
	}
	controlDir := filepath.Join("/tmp", "rmkssh-"+strconv.Itoa(os.Getuid()))
	if err := os.MkdirAll(controlDir, 0o700); err != nil {
		return opts
	}
	return append(opts,
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=10m",
		"-o", "ControlPath="+filepath.Join(controlDir, "%C"),
	)
}

type commandRunnerFunc func(name string, args ...string) error

func (f commandRunnerFunc) Run(name string, args ...string) error {
	return f(name, args...)
}

func (f commandRunnerFunc) Output(name string, args ...string) ([]byte, error) {
	return nil, fmt.Errorf("commandRunnerFunc does not support output")
}
