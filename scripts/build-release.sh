#!/usr/bin/env bash
set -euo pipefail

version="${1:-dev}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$repo_root/dist"
mkdir -p "$dist_dir"
rm -f "$dist_dir"/remork "$dist_dir"/remorkd-* "$dist_dir"/checksums.txt

build_daemon() {
  local goos="$1"
  local goarch="$2"
  local out="$dist_dir/remorkd-$goos-$goarch"
  echo "building $out"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.version=$version" \
    -o "$out" ./cmd/remorkd
}

go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$dist_dir/remork" ./cmd/remork
build_daemon linux amd64
build_daemon linux arm64
build_daemon darwin amd64
build_daemon darwin arm64

cp "$repo_root/deploy/remorkd.example.toml" "$dist_dir/remorkd.example.toml"
(
  cd "$dist_dir"
  shasum -a 256 remork remorkd-* remorkd.example.toml > checksums.txt
)
