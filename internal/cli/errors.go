package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/output"
)

func unboundWorkspaceError(err error) error {
	fix := "run remork init HOST:/absolute/remote/workspace, or cd into a directory that already has a remork binding"
	if errors.Is(err, os.ErrNotExist) {
		return codedCommandError{
			code: exitcode.InvalidUsageOrConfig,
			err:  fmt.Errorf("this directory is not bound to a remork workspace; run remork init HOST:/absolute/remote/workspace or cd into a bound workspace"),
			fix:  fix,
		}
	}
	return codedCommandError{
		code: exitcode.InvalidUsageOrConfig,
		err:  fmt.Errorf("current directory is not bound to a remork workspace: %w", err),
		fix:  fix,
	}
}

type silentCommandError struct {
	err error
}

func (e silentCommandError) Error() string {
	return e.err.Error()
}

func (e silentCommandError) Unwrap() error {
	return e.err
}

func (e silentCommandError) ExitCode() int {
	if coded, ok := e.err.(interface{ ExitCode() int }); ok {
		return coded.ExitCode()
	}
	return exitcode.GeneralError
}

func (e silentCommandError) Silent() bool {
	return true
}

func writeConflictSummary(cmd *cobra.Command, operation string, count int) {
	r := plainErrRenderer(cmd, false)
	r.Error(fmt.Sprintf("%s found %d conflict(s)", operation, count), "run remork status, then remork conflict -- <path> or remork restore -- <path>")
}

type commandErrorJSON struct {
	Error string `json:"error"`
	Fix   string `json:"fix,omitempty"`
	Code  int    `json:"code"`
}

func writeJSONCommandError(cmd *cobra.Command, err error) error {
	if writeErr := output.WriteJSON(cmd.OutOrStdout(), commandErrorJSON{
		Error: err.Error(),
		Fix:   commandErrorFix(err),
		Code:  commandErrorExitCode(err),
	}); writeErr != nil {
		return writeErr
	}
	return silentCommandError{err: err}
}

func WriteCommandError(w io.Writer, err error) {
	fmt.Fprintln(w, err)
	if fix := commandErrorFix(err); fix != "" {
		fmt.Fprintln(w, "fix: "+fix)
	}
}

func commandErrorExitCode(err error) int {
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return exitcode.GeneralError
}

func commandErrorFix(err error) string {
	var fixable interface{ Fix() string }
	if errors.As(err, &fixable) {
		return fixable.Fix()
	}
	return ""
}
