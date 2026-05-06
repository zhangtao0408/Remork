package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const releaseBaseURL = "https://github.com/zhangtao0408/Remork/releases/download"
const daemonVendorDirEnv = "REMORK_DAEMON_VENDOR_DIR"

type assetDownloader interface {
	Download(ctx context.Context, url, dst string) error
}

type defaultReleaseDownloader struct {
	client *http.Client
}

type releaseBinaryOptions struct {
	Version    string
	HomeDir    string
	Platform   string
	LocalBin   string
	Downloader assetDownloader
}

func resolveReleaseDaemonBinary(ctx context.Context, opts releaseBinaryOptions) (string, error) {
	if opts.LocalBin != "" {
		return opts.LocalBin, nil
	}
	platform := opts.Platform
	if platform == "" {
		platform = runtime.GOOS + "-" + runtime.GOARCH
		if runtime.GOOS != "linux" {
			return "", fmt.Errorf("could not select remorkd platform from %s; pass --platform linux-arm64 or linux-amd64", platform)
		}
	}
	if err := validateDaemonReleasePlatform(platform); err != nil {
		return "", err
	}
	name := "remorkd-" + platform

	if vendorPath := vendorDaemonBinaryPath(os.Getenv(daemonVendorDirEnv), name); vendorPath != "" {
		return vendorPath, nil
	}

	version := opts.Version
	if version == "" {
		version = "dev"
	}

	distPath := filepath.Join("dist", name)
	if fileExists(distPath) {
		return distPath, nil
	}
	if opts.HomeDir == "" {
		return "", fmt.Errorf("home directory is required to cache remorkd release binary")
	}

	cachePath := filepath.Join(opts.HomeDir, ".cache", "remork", "releases", version, name)
	if fileExists(cachePath) {
		return cachePath, nil
	}
	if version == "dev" {
		return "", fmt.Errorf("cannot resolve remorkd release binary for version dev; pass --local-bin or build %s into dist/ or the local remork cache", name)
	}
	downloader := opts.Downloader
	if downloader == nil {
		downloader = defaultReleaseDownloader{}
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", err
	}
	url := releaseBaseURL + "/" + version + "/" + name
	if err := downloader.Download(ctx, url, cachePath); err != nil {
		return "", err
	}
	if err := os.Chmod(cachePath, 0o755); err != nil {
		return "", err
	}
	return cachePath, nil
}

func (d defaultReleaseDownloader) Download(ctx context.Context, url, dst string) error {
	client := d.client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func vendorDaemonBinaryPath(vendorDir, name string) string {
	vendorDir = strings.TrimSpace(vendorDir)
	if vendorDir == "" {
		return ""
	}
	path := filepath.Join(vendorDir, name)
	if fileExists(path) {
		return path
	}
	return ""
}

func validateDaemonReleasePlatform(platform string) error {
	if platform == "" {
		return fmt.Errorf("daemon release platform is required")
	}
	if platform == "." || platform == ".." || filepath.Base(platform) != platform {
		return fmt.Errorf("invalid daemon release platform %q", platform)
	}
	switch platform {
	case "linux-arm64", "linux-amd64":
		return nil
	default:
		return fmt.Errorf("could not select remorkd platform from %s; pass --platform linux-arm64 or linux-amd64", platform)
	}
}
