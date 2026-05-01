package cli

import (
	"os"
	"os/exec"
)

type commandRunner interface {
	Run(name string, args ...string) error
}

type osCommandRunner struct{}

func (osCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type commandRunnerFunc func(name string, args ...string) error

func (f commandRunnerFunc) Run(name string, args ...string) error {
	return f(name, args...)
}
