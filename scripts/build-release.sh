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
cat > "$dist_dir/RELEASE_NOTES.md" <<EOF
# Remork $version

This is the first product release of Remork.

## Download the right asset

- macOS client, Apple Silicon: \`remork-$version-darwin-arm64.tar.gz\`
- macOS client, Intel: \`remork-$version-darwin-amd64.tar.gz\`
- Linux server daemon, arm64: \`remorkd-$version-linux-arm64.tar.gz\`
- Linux server daemon, amd64: \`remorkd-$version-linux-amd64.tar.gz\`
- Integrity checks: \`checksums.txt\`

The server host only needs the extracted \`remorkd\` binary. It does not need Go,
npm, apt, brew, or internet access.

## Quick start

1. Extract the macOS client package on your local machine and put \`remork\` in
   your PATH.
2. Extract the matching Linux server package and copy \`remorkd\` to the remote
   host.
3. Start \`remorkd --root /remote/workspace --addr 0.0.0.0:17731\` on a trusted
   VPN/private network.
4. Configure the local CLI with \`remork host add\`, then run \`remork init\` and
   \`remork sync\`.

Warning: \`--addr 0.0.0.0:17731\` without \`--token-file\` allows any client
that can reach the host to use file, apply, exec, and shell endpoints. Use it
only on a trusted VPN or private network. On shared networks, start the daemon
with \`--token-file /path/to/remork.token\` and configure the local host entry
with \`remork host add --token-env\`.
EOF
cat > "$dist_dir/README-release.md" <<EOF
# Remork $version release

This directory contains prebuilt Remork Product V1 binaries.

## GitHub release assets

- \`remork-$version-darwin-arm64.tar.gz\`: macOS client for Apple Silicon
- \`remork-$version-darwin-amd64.tar.gz\`: macOS client for Intel Macs
- \`remorkd-$version-linux-arm64.tar.gz\`: Linux server daemon for arm64 hosts
- \`remorkd-$version-linux-amd64.tar.gz\`: Linux server daemon for amd64 hosts
- \`checksums.txt\`: SHA-256 checksums for binaries, packages, and docs

Raw cross-compiled binaries are also left in \`dist/\` for local testing and
manual deployment:

- \`remork-darwin-arm64\`
- \`remork-darwin-amd64\`
- \`remork-linux-arm64\`
- \`remork-linux-amd64\`
- \`remorkd-darwin-arm64\`
- \`remorkd-darwin-amd64\`
- \`remorkd-linux-arm64\`
- \`remorkd-linux-amd64\`

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

package_binary() {
  local binary="$1"
  local archive="$2"
  local install_name="$3"
  local package_dir="$dist_dir/package-$archive"

  rm -rf "$package_dir"
  mkdir -p "$package_dir"
  install -m 0755 "$dist_dir/$binary" "$package_dir/$install_name"
  cp "$dist_dir/README-release.md" "$package_dir/README.md"
  if [[ "$install_name" == "remorkd" ]]; then
    cp "$dist_dir/remorkd.example.toml" "$package_dir/remorkd.example.toml"
  fi
  (
    cd "$package_dir"
    tar -czf "$dist_dir/$archive" .
  )
  rm -rf "$package_dir"
}

package_binary "remork-darwin-arm64" "remork-$version-darwin-arm64.tar.gz" "remork"
package_binary "remork-darwin-amd64" "remork-$version-darwin-amd64.tar.gz" "remork"
package_binary "remorkd-linux-arm64" "remorkd-$version-linux-arm64.tar.gz" "remorkd"
package_binary "remorkd-linux-amd64" "remorkd-$version-linux-amd64.tar.gz" "remorkd"

(
  cd "$dist_dir"
  shasum -a 256 remork-* remorkd-* remorkd.example.toml README-release.md RELEASE_NOTES.md > checksums.txt
)
