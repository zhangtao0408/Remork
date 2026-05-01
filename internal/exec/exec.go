package execx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"remork/internal/limits"
)

type Options struct {
	Context        context.Context
	Cwd            string
	Command        []string
	Env            []string
	Timeout        time.Duration
	MaxOutputBytes int64
}

type Result struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	TimedOut        bool   `json:"timed_out"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
}

type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}

func Run(opts Options) (Result, error) {
	if len(opts.Command) == 0 {
		return Result{ExitCode: -1}, errors.New("empty command")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = limits.DefaultExecTimeout
	}
	parent := opts.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	configureCommandForCancellation(cmd)
	cmd.WaitDelay = limits.DefaultExecWaitDelay
	cmd.Dir = opts.Cwd
	cmd.Env = append(os.Environ(), opts.Env...)
	maxOutput := opts.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = limits.MaxExecOutputBytes
	}
	stdout := &cappedBuffer{limit: maxOutput}
	stderr := &cappedBuffer{limit: maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	cleanupCommandGroup(cmd)
	if errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil && parent.Err() == nil {
		err = nil
	}
	res := Result{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		res.TimedOut = true
		return res, ctx.Err()
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else {
		res.ExitCode = -1
	}
	if err := parent.Err(); err != nil {
		if res.ExitCode == 0 {
			res.ExitCode = -1
		}
		return res, err
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		res.TimedOut = true
		return res, ctx.Err()
	}
	return res, err
}
