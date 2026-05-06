#!/usr/bin/env bash
set -euo pipefail

tag="${1:-dev}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$repo_root/dist"
pkg_dir="$repo_root/npm/remork"
vendor_dir="$pkg_dir/vendor"

npm_version="$tag"
if [[ "$npm_version" == v[0-9]* ]]; then
  npm_version="${npm_version#v}"
fi
if [[ "$npm_version" == "dev" || "$npm_version" == "ci" ]]; then
  npm_version="0.0.0-$npm_version"
fi

required=(
  remork-darwin-arm64
  remork-darwin-amd64
  remork-windows-arm64.exe
  remork-windows-amd64.exe
  remorkd-linux-arm64
  remorkd-linux-amd64
)

for asset in "${required[@]}"; do
  if [[ ! -f "$dist_dir/$asset" ]]; then
    echo "missing dist asset: $dist_dir/$asset" >&2
    echo "run scripts/build-release.sh $tag first" >&2
    exit 1
  fi
done

rm -rf "$vendor_dir"
mkdir -p "$vendor_dir"
for asset in "${required[@]}"; do
  cp "$dist_dir/$asset" "$vendor_dir/$asset"
done
chmod 0755 "$vendor_dir"/remork-* "$vendor_dir"/remorkd-*

cat > "$pkg_dir/package.json" <<EOF
{
  "name": "@zhangtao0408/remork",
  "version": "$npm_version",
  "description": "Remote workspace control for private servers",
  "bin": {
    "remork": "bin/remork.js"
  },
  "os": [
    "darwin",
    "win32"
  ],
  "cpu": [
    "arm64",
    "x64"
  ],
  "scripts": {
    "test": "node --test test/*.test.js"
  },
  "files": [
    "bin/",
    "vendor/",
    "README.md"
  ],
  "engines": {
    "node": ">=18"
  },
  "repository": {
    "type": "git",
    "url": "git+https://github.com/zhangtao0408/Remork.git"
  },
  "homepage": "https://github.com/zhangtao0408/Remork#readme",
  "bugs": {
    "url": "https://github.com/zhangtao0408/Remork/issues"
  },
  "license": "UNLICENSED"
}
EOF

cat > "$pkg_dir/README.md" <<EOF
# Remork

Remote workspace control for private servers.

## Install

\`\`\`bash
npm install -g @zhangtao0408/remork@beta
remork setup
\`\`\`

This package includes Remork client binaries for macOS and Windows plus Linux
\`remorkd\` daemon binaries used by \`remork setup\`.

## Security and Network Safety

Remork is intended for trusted private networks, VPNs, or similarly controlled
server environments. \`remork setup\` installs or updates a remote HTTP daemon;
do not expose that daemon directly to untrusted networks. When a daemon is
reachable from a shared network, enable token authentication and keep the token
private.

Supported client platforms:

- macOS arm64
- macOS amd64
- Windows arm64
- Windows amd64

Supported server daemon platforms:

- Linux arm64
- Linux amd64

For full documentation, see https://github.com/zhangtao0408/Remork.
EOF

(cd "$pkg_dir" && npm pack --dry-run)
