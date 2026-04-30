package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"remork/internal/daemon"
)

var version = "dev"

func main() {
	addr := flag.String("addr", "127.0.0.1:7731", "listen address")
	root := flag.String("root", "", "workspace root")
	operationLog := flag.String("operation-log", "/tmp/remorkd-operations.jsonl", "operation log JSONL path")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("remorkd " + version)
		return
	}
	if *root == "" {
		log.Fatal("--root is required")
	}
	srv := daemon.NewServer(daemon.Config{Roots: []string{*root}, LargeThreshold: 128 << 20, OperationLogPath: *operationLog})
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
