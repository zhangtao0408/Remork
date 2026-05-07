package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"remork/internal/config"
	"remork/internal/remoteroot"
)

type ConnectSpec struct {
	URL           string
	HostName      string
	Token         string
	TokenEnv      string
	TokenFile     string
	NoProxy       bool
	SelectedRoot  string
	WorkspacePath string
	LocalRoot     string
	FirstSync     bool
}

var nonHostNameChars = regexp.MustCompile(`[^A-Za-z0-9]+`)

func deriveConnectHostName(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("daemon URL must include a host")
	}
	host := parsed.Hostname()
	if port := parsed.Port(); port != "" {
		host += "-" + port
	}
	name := strings.Trim(nonHostNameChars.ReplaceAllString(host, "-"), "-")
	if name == "" {
		return "", fmt.Errorf("could not derive host name from %q", rawURL)
	}
	return strings.ToLower(name), nil
}

func defaultConnectTokenFile(homeDir, hostName string) string {
	return filepath.Join(homeDir, ".remork", "tokens", hostName+".token")
}

func writeConnectTokenFile(path, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600)
}

func ExecuteConnectSpec(opts Options, spec ConnectSpec) error {
	if err := validateDaemonURL(spec.URL); err != nil {
		return err
	}
	if spec.HostName == "" {
		name, err := deriveConnectHostName(spec.URL)
		if err != nil {
			return err
		}
		spec.HostName = name
	}
	if spec.LocalRoot == "" {
		spec.LocalRoot = opts.WorkingDir
	}
	if spec.LocalRoot == "" {
		return fmt.Errorf("local root is required")
	}
	if opts.DaemonProbe == nil {
		opts.DaemonProbe = httpDaemonProbe{}
	}
	if spec.Token != "" && spec.TokenEnv == "" && spec.TokenFile == "" {
		spec.TokenFile = defaultConnectTokenFile(opts.HomeDir, spec.HostName)
	}
	if err := writeConnectTokenFile(spec.TokenFile, spec.Token); err != nil {
		return err
	}

	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	host := config.Host{Name: spec.HostName, URL: spec.URL, TokenEnv: spec.TokenEnv, TokenFile: spec.TokenFile, NoProxy: spec.NoProxy}
	status, err := opts.DaemonProbe.Status(context.Background(), host, cfg.ClientID)
	if err != nil {
		return err
	}
	if len(status.Roots) == 0 {
		return fmt.Errorf("daemon did not advertise any allowed roots")
	}
	allowed, err := remoteroot.NormalizeMany(status.Roots)
	if err != nil {
		return err
	}
	selected := spec.SelectedRoot
	if selected == "" {
		selected = status.Roots[0]
	}
	remoteRoot, err := remoteroot.ResolveWorkspacePath(allowed, selected, spec.WorkspacePath)
	if err != nil {
		return err
	}
	if _, err := opts.DaemonProbe.Manifest(context.Background(), host, cfg, remoteRoot); err != nil {
		return err
	}
	cfg.Hosts[spec.HostName] = host
	if err := store.Save(cfg); err != nil {
		return err
	}
	return ExecuteWorkspaceBindSpec(opts, WorkspaceBindSpec{
		HostName:   spec.HostName,
		RemoteRoot: remoteRoot,
		LocalRoot:  spec.LocalRoot,
	})
}
