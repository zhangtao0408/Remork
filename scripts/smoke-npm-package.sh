#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pkg_dir="$repo_root/npm/remork"
tmp_dir="$(mktemp -d)"
tarball_path=""

cleanup() {
  rm -rf "$tmp_dir"
  if [[ -n "$tarball_path" ]]; then
    rm -f "$tarball_path"
  fi
}
trap cleanup EXIT

(cd "$pkg_dir" && npm test)
(cd "$pkg_dir" && npm pack --dry-run)

if [[ "$(uname -s)" == "Darwin" ]]; then
  tarball="$(cd "$pkg_dir" && npm pack --silent)"
  tarball_path="$pkg_dir/$tarball"
  npm install -g "$pkg_dir/$tarball" --prefix "$tmp_dir/npm-global"
  "$tmp_dir/npm-global/bin/remork" version
  "$tmp_dir/npm-global/bin/remork" setup --help >/dev/null
  "$tmp_dir/npm-global/bin/remork" daemon install --help >/dev/null
else
  echo "Skipping install-level smoke on $(uname -s): npm client package supports macOS and Windows only."
fi
