package main

import (
	"fmt"
	"os"

	"remork/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.NewRootCommand(cli.Options{Version: version}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
