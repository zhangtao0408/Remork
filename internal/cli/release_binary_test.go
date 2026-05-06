package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeAssetDownloader struct {
	urls []string
}

func (f *fakeAssetDownloader) Download(ctx context.Context, url, dst string) error {
	f.urls = append(f.urls, url)
	return os.WriteFile(dst, []byte("daemon"), 0o644)
}

func TestResolveReleaseDaemonBinaryLocalBinWinsForDev(t *testing.T) {
	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "dev",
		HomeDir:  t.TempDir(),
		LocalBin: "/custom/remorkd",
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != "/custom/remorkd" {
		t.Fatalf("path = %q, want local bin", got)
	}
}

func TestResolveReleaseDaemonBinaryUsesDistWhenPresent(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join("dist", "remorkd-linux-arm64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write dist binary: %v", err)
	}

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "v1.2.3",
		HomeDir:  t.TempDir(),
		Platform: "linux-arm64",
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != filepath.Join("dist", "remorkd-linux-arm64") {
		t.Fatalf("path = %q, want dist binary", got)
	}
}

func TestResolveReleaseDaemonBinaryUsesCachedRelease(t *testing.T) {
	home := t.TempDir()
	cachePath := filepath.Join(home, ".cache", "remork", "releases", "v1.2.3", "remorkd-linux-arm64")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write cache binary: %v", err)
	}
	downloader := &fakeAssetDownloader{}

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:    "v1.2.3",
		HomeDir:    home,
		Platform:   "linux-arm64",
		Downloader: downloader,
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != cachePath {
		t.Fatalf("path = %q, want cached binary", got)
	}
	if len(downloader.urls) != 0 {
		t.Fatalf("downloaded despite cached binary: %v", downloader.urls)
	}
}

func TestResolveReleaseDaemonBinaryDownloadsMissingRelease(t *testing.T) {
	home := t.TempDir()
	downloader := &fakeAssetDownloader{}

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:    "v1.2.3",
		HomeDir:    home,
		Platform:   "linux-arm64",
		Downloader: downloader,
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	wantPath := filepath.Join(home, ".cache", "remork", "releases", "v1.2.3", "remorkd-linux-arm64")
	if got != wantPath {
		t.Fatalf("path = %q, want %q", got, wantPath)
	}
	if len(downloader.urls) != 1 || downloader.urls[0] != releaseBaseURL+"/v1.2.3/remorkd-linux-arm64" {
		t.Fatalf("download urls = %v", downloader.urls)
	}
	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat downloaded binary: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("downloaded mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestResolveReleaseDaemonBinaryRejectsDevWithoutLocalBin(t *testing.T) {
	_, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "dev",
		HomeDir:  t.TempDir(),
		Platform: "linux-arm64",
	})
	if err == nil {
		t.Fatal("resolveReleaseDaemonBinary returned nil error, want dev rejection")
	}
}

func TestResolveReleaseDaemonBinaryUsesDistForDev(t *testing.T) {
	wd := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join("dist", "remorkd-linux-arm64"), []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write dist binary: %v", err)
	}

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:  "dev",
		HomeDir:  t.TempDir(),
		Platform: "linux-arm64",
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != filepath.Join("dist", "remorkd-linux-arm64") {
		t.Fatalf("path = %q, want dist binary", got)
	}
}

func TestResolveReleaseDaemonBinaryUsesCachedDevBinary(t *testing.T) {
	home := t.TempDir()
	cachePath := filepath.Join(home, ".cache", "remork", "releases", "dev", "remorkd-linux-arm64")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("daemon"), 0o755); err != nil {
		t.Fatalf("write cache binary: %v", err)
	}
	downloader := &fakeAssetDownloader{}

	got, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
		Version:    "dev",
		HomeDir:    home,
		Platform:   "linux-arm64",
		Downloader: downloader,
	})
	if err != nil {
		t.Fatalf("resolveReleaseDaemonBinary returned error: %v", err)
	}
	if got != cachePath {
		t.Fatalf("path = %q, want cached dev binary", got)
	}
	if len(downloader.urls) != 0 {
		t.Fatalf("dev binary should not be downloaded: %v", downloader.urls)
	}
}

func TestResolveReleaseDaemonBinaryRejectsInvalidPlatform(t *testing.T) {
	tests := []string{
		"linux/arm64",
		"../linux-arm64",
		"..",
		"darwin-arm64",
		"linux-riscv64",
	}
	for _, platform := range tests {
		t.Run(platform, func(t *testing.T) {
			_, err := resolveReleaseDaemonBinary(context.Background(), releaseBinaryOptions{
				Version:  "v1.2.3",
				HomeDir:  t.TempDir(),
				Platform: platform,
			})
			if err == nil {
				t.Fatalf("resolveReleaseDaemonBinary platform %q returned nil error, want rejection", platform)
			}
		})
	}
}

func TestValidateDaemonReleasePlatformAllowsLinuxAssets(t *testing.T) {
	for _, platform := range []string{"linux-arm64", "linux-amd64"} {
		t.Run(platform, func(t *testing.T) {
			if err := validateDaemonReleasePlatform(platform); err != nil {
				t.Fatalf("validateDaemonReleasePlatform(%q) returned error: %v", platform, err)
			}
		})
	}
}
