package main

import (
	"os"
	"strings"

	"remork/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.NewRootCommand(cli.Options{Version: displayVersion(version)}).Execute(); err != nil {
		if !isSilentError(err) {
			cli.WriteCommandError(os.Stderr, err)
		}
		os.Exit(commandExitCode(err))
	}
}

func displayVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "v") && len(raw) > 1 && raw[1] >= '0' && raw[1] <= '9' {
		return raw[1:]
	}
	return raw
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
