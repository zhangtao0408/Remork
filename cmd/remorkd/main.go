package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"remork/internal/daemon"
	"remork/internal/limits"
	"remork/internal/remorkdconfig"
	"remork/internal/safety"
	"remork/internal/tui"
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
		case "setup":
			if err := runSetupCommand(os.Args[2:]); err != nil {
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

func configFromSetupValues(home string, values map[string]string) (remorkdconfig.Config, string, error) {
	roots := splitComma(values["allowed_roots"])
	cfg := remorkdconfig.Config{
		ListenAddr:         firstValue(values["listen_addr"], "0.0.0.0:17731"),
		AllowedRoots:       roots,
		LargeFileThreshold: firstValue(values["large_file_threshold"], "128MB"),
		TokenFile:          remorkdconfig.ExpandHome(firstValue(values["token_file"], "$HOME/.remork/remork.token"), home),
		PIDFile:            remorkdconfig.ExpandHome(firstValue(values["pid_file"], "$HOME/.remork/run/remorkd.pid"), home),
		LogFile:            remorkdconfig.ExpandHome(firstValue(values["log_file"], "$HOME/.remork/log/remorkd.log"), home),
	}
	token := ""
	switch strings.TrimSpace(values["token_mode"]) {
	case "", "generate":
		token = randomToken()
	case "paste", "update":
		token = strings.TrimSpace(values["token"])
	case "none":
		cfg.TokenFile = ""
	default:
		return remorkdconfig.Config{}, "", fmt.Errorf("unknown token mode %q", values["token_mode"])
	}
	return cfg, token, remorkdconfig.Validate(cfg)
}

func splitComma(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func runSetupCommand(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := fs.String("config", remorkdconfig.DefaultPath(home), "remorkd config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	values, err := tui.RunForm(tui.NewFormModel("remorkd setup", []tui.Field{
		{Section: "Network", Key: "listen_addr", Label: "Listen address", Initial: "0.0.0.0:17731"},
		{Section: "Workspace", Key: "allowed_roots", Label: "Allowed roots", Placeholder: "/home/me, /scratch/me"},
		{Section: "Auth", Key: "token_mode", Label: "Token mode", Initial: "generate", Help: "generate, paste, update, or none"},
		{Section: "Auth", Key: "token", Label: "Token", Help: "Used only for paste or update mode."},
		{Section: "Auth", Key: "token_file", Label: "Token file", Initial: "$HOME/.remork/remork.token"},
		{Section: "Files", Key: "large_file_threshold", Label: "Large file threshold", Initial: "128MB"},
		{Section: "Files", Key: "pid_file", Label: "PID file", Initial: "$HOME/.remork/run/remorkd.pid"},
		{Section: "Files", Key: "log_file", Label: "Log file", Initial: "$HOME/.remork/log/remorkd.log"},
	}))
	if err != nil {
		return err
	}
	cfg, token, err := configFromSetupValues(home, values)
	if err != nil {
		return err
	}
	if token != "" && cfg.TokenFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.TokenFile), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(cfg.TokenFile, []byte(token+"\n"), 0o600); err != nil {
			return err
		}
	}
	if err := remorkdconfig.Save(*configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Config written: %s\n", *configPath)
	fmt.Printf("Start daemon: remorkd start --config %s\n", *configPath)
	fmt.Printf("Client connect: remork connect --url http://HOST:%s\n", daemonPort(cfg.ListenAddr))
	return nil
}

func daemonPort(addr string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err == nil && port != "" {
		return port
	}
	if idx := strings.LastIndex(addr, ":"); idx >= 0 && idx+1 < len(addr) {
		return addr[idx+1:]
	}
	return "17731"
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
