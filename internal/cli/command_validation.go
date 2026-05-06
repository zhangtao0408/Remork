package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"remork/internal/exitcode"
	"remork/internal/paths"
)

func exactArgsJSON(n int, jsonOut *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		return positionalArgsCommandError(cmd, jsonOut, n, len(args))
	}
}

func maxArgsJSON(n int, jsonOut *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= n {
			return nil
		}
		return positionalArgsCommandError(cmd, jsonOut, n, len(args))
	}
}

func noArgsJSON(jsonOut *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return positionalArgsCommandError(cmd, jsonOut, 0, len(args))
	}
}

func positionalArgsCommandError(cmd *cobra.Command, jsonOut *bool, want, got int) error {
	err := codedCommandError{
		code: exitcode.InvalidUsageOrConfig,
		err:  fmt.Errorf("accepts %d arg(s), received %d", want, got),
		fix:  "run " + cmd.CommandPath() + " --help",
	}
	if jsonOut != nil && *jsonOut {
		return writeJSONCommandError(cmd, err)
	}
	return err
}

func validateWorkspacePathArgs(values []string) error {
	for _, value := range values {
		if err := validateWorkspacePathArg(value); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkspacePathArg(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return nil
	}
	if _, err := paths.NormalizeRemotePath(value); err != nil {
		return workspacePathCommandError(value)
	}
	return nil
}

func workspacePathCommandError(value string) error {
	return codedCommandError{
		code: exitcode.InvalidUsageOrConfig,
		err:  fmt.Errorf("path must stay inside the bound workspace: %s", value),
		fix:  "use a relative path inside the bound workspace, for example remork sync src/file.txt",
	}
}

func pullTargetCommandError(err error) error {
	return codedCommandError{
		code: exitcode.InvalidUsageOrConfig,
		err:  err,
		fix:  "use a path inside the bound workspace, or a HOST:/remote/root/path target that matches the bound workspace",
	}
}
