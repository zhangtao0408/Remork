package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var ErrPromptRequired = errors.New("confirmation required")

type Options struct {
	Force bool
	Quiet bool
	In    io.Reader
	Out   io.Writer
}

func Confirm(opts Options, message string) (bool, error) {
	if opts.Force {
		return true, nil
	}
	if opts.Quiet {
		return false, ErrPromptRequired
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	if _, err := fmt.Fprintf(out, "%s [y/N] ", message); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.TrimSpace(line)
	return answer == "y" || answer == "Y", nil
}
