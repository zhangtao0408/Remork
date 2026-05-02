package cli

import (
	"fmt"
	"os"
	"os/exec"
)

type commandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (osCommandRunner) Output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	return cmd.CombinedOutput()
}

type commandRunnerFunc func(name string, args ...string) error

func (f commandRunnerFunc) Run(name string, args ...string) error {
	return f(name, args...)
}

func (f commandRunnerFunc) Output(name string, args ...string) ([]byte, error) {
	return nil, fmt.Errorf("commandRunnerFunc does not support output")
}
