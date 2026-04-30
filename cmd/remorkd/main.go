package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"remork/internal/daemon"
)

var version = "dev"

func main() {
	addr := flag.String("addr", "127.0.0.1:7731", "listen address")
	root := flag.String("root", "", "workspace root")
	token := flag.String("token", "", "shared bearer token")
	tokenFile := flag.String("token-file", "", "file containing shared bearer token")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("remorkd " + version)
		return
	}
	if *root == "" {
		log.Fatal("--root is required")
	}
	if *token != "" && *tokenFile != "" {
		log.Fatal("--token and --token-file cannot both be set")
	}
	resolvedToken := *token
	if *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			log.Fatal(err)
		}
		resolvedToken = strings.TrimSpace(string(data))
	}
	srv := daemon.NewServer(daemon.Config{Version: version, Roots: []string{*root}, LargeThreshold: 128 << 20, Token: resolvedToken})
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
