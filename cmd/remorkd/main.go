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
	resolvedToken, err := resolveToken(*token, *tokenFile)
	if err != nil {
		log.Fatal(err)
	}
	srv := daemon.NewServer(daemon.Config{Version: version, Roots: []string{*root}, LargeThreshold: 128 << 20, Token: resolvedToken})
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}

func resolveToken(token, tokenFile string) (string, error) {
	if token != "" && tokenFile != "" {
		return "", fmt.Errorf("--token and --token-file cannot both be set")
	}
	if tokenFile == "" {
		return token, nil
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", err
	}
	resolved := strings.TrimSpace(string(data))
	if resolved == "" {
		return "", fmt.Errorf("token file %q is empty", tokenFile)
	}
	return resolved, nil
}
