package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"remork/internal/daemon"
	"remork/internal/limits"
	"remork/internal/safety"
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
	if insecureNoTokenNonLoopbackListenAddr(*addr, resolvedToken != "") {
		log.Printf("WARNING: remorkd is listening on a non-loopback or wildcard address without authentication; clients that can reach it can use apply/file access and writes, remote command execution, and shell endpoints. Use --token-file and configure clients with remork host add --token-env.")
	}
	srv := daemon.NewServer(daemon.Config{Version: version, Roots: []string{*root}, LargeThreshold: 128 << 20, Token: resolvedToken})
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: limits.DaemonReadHeaderTimeout,
		IdleTimeout:       limits.DaemonIdleTimeout,
	}
	log.Fatal(httpServer.ListenAndServe())
}

func insecureNoTokenNonLoopbackListenAddr(addr string, hasToken bool) bool {
	return safety.UnsafeNoTokenNonLoopbackBind(addr, hasToken)
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
