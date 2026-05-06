package main

import (
	"os"

	"remork/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.NewRootCommand(cli.Options{Version: version}).Execute(); err != nil {
		if !isSilentError(err) {
			cli.WriteCommandError(os.Stderr, err)
		}
		os.Exit(commandExitCode(err))
	}
}

func commandExitCode(err error) int {
	if coded, ok := err.(interface{ ExitCode() int }); ok {
		return coded.ExitCode()
	}
	return 1
}

func isSilentError(err error) bool {
	if silent, ok := err.(interface{ Silent() bool }); ok {
		return silent.Silent()
	}
	return false
}
