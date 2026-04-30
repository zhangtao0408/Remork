package execx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

type Options struct {
	Cwd     string
	Command []string
	Env     []string
	Timeout time.Duration
}

type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

func Run(opts Options) (Result, error) {
	if len(opts.Command) == 0 {
		return Result{ExitCode: -1}, errors.New("empty command")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = append(os.Environ(), opts.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		res.TimedOut = true
		return res, ctx.Err()
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	return res, err
}
