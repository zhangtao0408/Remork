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
	"remork/internal/remorkdconfig"
	"remork/internal/safety"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			if err := runServeCommand(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		}
	}
	addr := flag.String("addr", "127.0.0.1:7731", "listen address")
	var roots rootFlags
	flag.Var(&roots, "root", "allowed base root; repeat to serve multiple base roots")
	token := flag.String("token", "", "shared bearer token")
	tokenFile := flag.String("token-file", "", "file containing shared bearer token")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("remorkd " + version)
		return
	}
	if len(roots) == 0 {
		log.Fatal("--root is required")
	}
	resolvedToken, err := resolveToken(*token, *tokenFile)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(runServer(serverOptions{Addr: *addr, Roots: []string(roots), Token: resolvedToken, Version: version}))
}

type serverOptions struct {
	Addr    string
	Roots   []string
	Token   string
	Version string
}

func serverOptionsFromConfig(cfg remorkdconfig.Config, version string) (serverOptions, error) {
	token, err := resolveToken("", cfg.TokenFile)
	if err != nil {
		return serverOptions{}, err
	}
	return serverOptions{Addr: cfg.ListenAddr, Roots: cfg.AllowedRoots, Token: token, Version: version}, nil
}

func runServer(opts serverOptions) error {
	if len(opts.Roots) == 0 {
		return fmt.Errorf("--root is required")
	}
	if insecureNoTokenNonLoopbackListenAddr(opts.Addr, opts.Token != "") {
		log.Printf("WARNING: remorkd is listening on a non-loopback or wildcard address without authentication; clients that can reach it can use apply/file access and writes, remote command execution, and shell endpoints. Use --token-file and configure clients with remork connect.")
	}
	srv := daemon.NewServer(daemon.Config{Version: opts.Version, Roots: opts.Roots, LargeThreshold: 128 << 20, Token: opts.Token})
	httpServer := &http.Server{
		Addr:              opts.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: limits.DaemonReadHeaderTimeout,
		IdleTimeout:       limits.DaemonIdleTimeout,
	}
	return httpServer.ListenAndServe()
}

func runServeCommand(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := fs.String("config", remorkdconfig.DefaultPath(home), "remorkd config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := remorkdconfig.Load(*configPath, home)
	if err != nil {
		return err
	}
	opts, err := serverOptionsFromConfig(cfg, version)
	if err != nil {
		return err
	}
	return runServer(opts)
}

type rootFlags []string

func (r *rootFlags) String() string {
	return strings.Join(*r, ",")
}

func (r *rootFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("--root cannot be empty")
	}
	*r = append(*r, value)
	return nil
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
