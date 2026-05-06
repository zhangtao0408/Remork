#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pkg_dir="$repo_root/npm/remork"

if [[ $# -gt 0 && "$1" != --* ]]; then
  pkg_dir="$1"
  shift
fi
pkg_dir="$(cd "$pkg_dir" && pwd)"

package_name="$(node -p 'require(require("node:path").resolve(process.argv[1], "package.json")).name' "$pkg_dir")"
version="$(node -p 'require(require("node:path").resolve(process.argv[1], "package.json")).version' "$pkg_dir")"
tag="${NPM_TAG:-}"
publish_args=()

if [[ "$package_name" == @*/* ]]; then
  publish_args+=(--access public)
fi

if [[ "$version" == *-* ]]; then
  tag="${tag:-beta}"
  if [[ "$tag" == "latest" ]]; then
    echo "refusing to publish prerelease $version with npm dist-tag 'latest'" >&2
    echo "set NPM_TAG=beta or another prerelease dist-tag" >&2
    exit 1
  fi
  publish_args+=(--tag "$tag")
elif [[ -n "$tag" ]]; then
  publish_args+=(--tag "$tag")
fi

exec npm publish "$pkg_dir" "${publish_args[@]}" "$@"
