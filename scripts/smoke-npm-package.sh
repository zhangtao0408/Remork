#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pkg_dir="$repo_root/npm/remork"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

(cd "$pkg_dir" && npm test)
(cd "$pkg_dir" && npm pack --dry-run)

if [[ "$(uname -s)" == "Darwin" ]]; then
  tarball="$(cd "$pkg_dir" && npm pack --silent)"
  npm install -g "$pkg_dir/$tarball" --prefix "$tmp_dir/npm-global"
  "$tmp_dir/npm-global/bin/remork" version
  "$tmp_dir/npm-global/bin/remork" setup --help >/dev/null
  "$tmp_dir/npm-global/bin/remork" daemon install --help >/dev/null
fi
