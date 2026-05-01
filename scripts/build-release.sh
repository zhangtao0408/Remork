#!/usr/bin/env bash
set -euo pipefail

version="${1:-dev}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$repo_root/dist"
rm -rf "$dist_dir"
mkdir -p "$dist_dir"

build_binary() {
  local package="$1"
  local name="$2"
  local goos="$3"
  local goarch="$4"
  local out="$dist_dir/$name-$goos-$goarch"
  echo "building $out"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.version=$version" \
    -o "$out" "$package"
}

build_target() {
  local goos="$1"
  local goarch="$2"
  build_binary ./cmd/remork remork "$goos" "$goarch"
  build_binary ./cmd/remorkd remorkd "$goos" "$goarch"
}

build_target darwin arm64
build_target darwin amd64
build_target linux arm64
build_target linux amd64

cp "$repo_root/deploy/remorkd.example.toml" "$dist_dir/remorkd.example.toml"
cat > "$dist_dir/README-release.md" <<EOF
# Remork $version release

This directory contains prebuilt Remork Product V1 binaries.

## Binaries

- remork-darwin-arm64
- remork-darwin-amd64
- remork-linux-arm64
- remork-linux-amd64
- remorkd-darwin-arm64
- remorkd-darwin-amd64
- remorkd-linux-arm64
- remorkd-linux-amd64

Copy only the matching remorkd binary to a remote host. The remote host does not
need Go, npm, apt, brew, or internet access.

## Quick remote start

\`\`\`bash
scp remorkd-linux-arm64 user@host:/tmp/remorkd
ssh user@host 'chmod +x /tmp/remorkd && nohup /tmp/remorkd --root /data/project --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo \$! >/tmp/remorkd.pid'
curl --noproxy '*' http://host:17731/status
\`\`\`

Warning: \`--addr 0.0.0.0:17731\` without \`--token-file\` allows any client
that can reach the host to use file, apply, exec, and shell endpoints. Use it
only on a trusted VPN or private network. On shared networks, start the daemon
with \`--token-file /path/to/remork.token\` and configure the local host entry
with the same token.

Verify downloads with checksums.txt.
EOF
(
  cd "$dist_dir"
  shasum -a 256 remork-* remorkd-* remorkd.example.toml README-release.md > checksums.txt
)
