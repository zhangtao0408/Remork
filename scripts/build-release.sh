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
cat > "$dist_dir/RELEASE_BODY.md" <<EOF
# Remork $version

This is the first product release of Remork.

## Download the right asset

- macOS client, Apple Silicon: \`remork-darwin-arm64\`
- macOS client, Intel: \`remork-darwin-amd64\`
- Linux server daemon, arm64: \`remorkd-linux-arm64\`
- Linux server daemon, amd64: \`remorkd-linux-amd64\`

The server host only needs the downloaded \`remorkd\` binary. It does not need
Go, npm, apt, brew, or internet access.

## Install the macOS client

\`\`\`bash
curl -L -o remork https://github.com/zhangtao0408/Remork/releases/download/$version/remork-darwin-arm64
chmod 0755 remork
mkdir -p ~/bin
mv remork ~/bin/remork
remork version
\`\`\`

Use \`remork-darwin-amd64\` instead on Intel Macs.

## Install the server daemon

\`\`\`bash
curl -L -o remorkd https://github.com/zhangtao0408/Remork/releases/download/$version/remorkd-linux-arm64
chmod 0755 remorkd
scp remorkd user@host:/tmp/remorkd
ssh user@host 'nohup /tmp/remorkd --root /data/project --addr 0.0.0.0:17731 </dev/null >/tmp/remorkd.log 2>&1 & echo \$! >/tmp/remorkd.pid'
\`\`\`

Use \`remorkd-linux-amd64\` instead on x86_64 Linux servers.

## Use Remork

\`\`\`bash
remork host add lab-a --url http://10.0.0.12:17731
mkdir project-a && cd project-a
remork init lab-a:/data/project
remork sync
remork status
\`\`\`

Read the full usage guide in the repository README:
https://github.com/zhangtao0408/Remork/blob/main/README.md

Warning: \`--addr 0.0.0.0:17731\` without \`--token-file\` allows any client
that can reach the host to use file, apply, exec, and shell endpoints. Use it
only on a trusted VPN or private network. On shared networks, start the daemon
with \`--token-file /path/to/remork.token\` and configure the local host entry
with \`remork host add --token-env\`.
EOF

(
  cd "$dist_dir"
  shasum -a 256 remork-* remorkd-* remorkd.example.toml > checksums.txt
  {
    echo
    echo "## SHA-256 checksums"
    echo
    echo '```text'
    grep -E ' (remork-darwin-arm64|remork-darwin-amd64|remorkd-linux-arm64|remorkd-linux-amd64)$' checksums.txt
    echo '```'
  } >> RELEASE_BODY.md
)
