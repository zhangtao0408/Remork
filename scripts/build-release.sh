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

build_binary ./cmd/remork remork darwin arm64
build_binary ./cmd/remork remork darwin amd64
build_binary ./cmd/remorkd remorkd linux arm64
build_binary ./cmd/remorkd remorkd linux amd64

cat > "$dist_dir/RELEASE_BODY.md" <<EOF
# Remork $version

This is a Product V1 beta release of Remork.

## Download the right asset

- macOS client, Apple Silicon: \`remork-darwin-arm64\`
- macOS client, Intel: \`remork-darwin-amd64\`
- Linux server daemon, arm64: \`remorkd-linux-arm64\`
- Linux server daemon, amd64: \`remorkd-linux-amd64\`

The server host only needs the downloaded \`remorkd\` binary. It does not need
Go, npm, apt, brew, or internet access.

Only upload the four binaries above as release assets. Keep this release body
as the install guide; do not upload README files for each release.

## Install the macOS client

\`\`\`bash
mkdir -p "\$HOME/.local/bin"
curl -L -o "\$HOME/.local/bin/remork" https://github.com/zhangtao0408/Remork/releases/download/$version/remork-darwin-arm64
chmod 0755 "\$HOME/.local/bin/remork"
export PATH="\$HOME/.local/bin:\$PATH"
remork version
\`\`\`

Use \`remork-darwin-amd64\` instead on Intel Macs.

If a new terminal cannot find \`remork\`, add \`export PATH="\$HOME/.local/bin:\$PATH"\` to \`~/.zshrc\`.

## Install the server daemon

Install the daemon from the client machine. This copies the prebuilt daemon over
SSH, stores it under the remote user's \`~/.local/bin/remorkd\`, stores pid/log
files under \`~/.remork\`, writes the local host config, and verifies the daemon.

\`\`\`bash
HOST_ALIAS=my-server
SSH_TARGET=user@my-server
DAEMON_URL=http://remork-daemon.example.internal:17731
ALLOWED_ROOT=/home/me
REMOTE_PLATFORM=linux-arm64

remork daemon install "\$HOST_ALIAS" \\
  --ssh "\$SSH_TARGET" \\
  --url "\$DAEMON_URL" \\
  --root "\$ALLOWED_ROOT" \\
  --platform "\$REMOTE_PLATFORM" \\
  --execute --yes \\
  --verify \\
  --no-proxy
\`\`\`

\`--root\` is an allowed base root. Any workspace under that directory can be
bound later. Use \`REMOTE_PLATFORM=linux-amd64\` instead on x86_64 Linux servers.
Repeat \`--root\` if one daemon should advertise multiple independent allowed
base roots.
If \`DAEMON_URL\` uses a non-default port, pass the same port with
\`--addr 0.0.0.0:PORT\`.

## Use Remork

\`\`\`bash
HOST_ALIAS=my-server
WORKSPACE_ROOT=/home/me/project
LOCAL_WORKING_COPY=~/remork/project

mkdir -p "\$LOCAL_WORKING_COPY"
cd "\$LOCAL_WORKING_COPY"
remork init "\$HOST_ALIAS:\$WORKSPACE_ROOT"
remork sync
remork status
remork run -- pwd

# Edit files locally, then write those edits back to the remote workspace.
remork status
remork apply
remork log --limit 5
\`\`\`

Read the full usage guide in the repository README for this tag:
https://github.com/zhangtao0408/Remork/blob/$version/README.md

Warning: \`--addr 0.0.0.0:17731\` without \`--token-file\` allows any client
that can reach the host to use file, apply, exec, and shell endpoints. Use it
only on a trusted VPN or private network. On shared networks, start the daemon
with \`--token-file /path/to/remork.token\` and configure the local host entry
with \`remork host add --token-env\`.
EOF

(
  cd "$dist_dir"
  shasum -a 256 remork-* remorkd-* > checksums.txt
  {
    echo
    echo "## SHA-256 checksums"
    echo
    echo '```text'
    grep -E ' (remork-darwin-arm64|remork-darwin-amd64|remorkd-linux-arm64|remorkd-linux-amd64)$' checksums.txt
    echo '```'
  } >> RELEASE_BODY.md
)
