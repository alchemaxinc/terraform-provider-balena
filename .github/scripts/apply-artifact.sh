#!/usr/bin/env bash
# Copies the files an unprivileged job uploaded as an artifact into a trusted checkout, for the jobs that hold the
# GitHub App token. The artifact is untrusted data: only regular files under the allowed paths are accepted, and
# anything else (a symlink, a device, a path outside the list, an unusual file name) fails the run before any file is
# copied.
#
#   apply-artifact.sh <artifact-dir> <checkout-dir> <allowed-path>...
#
# Each allowed path is a file or a directory relative to the repository root and must be present in the artifact.
# A directory is mirrored: files the artifact no longer has are deleted from the checkout, so a regeneration can
# remove files. Nothing in the artifact is executed.
set -euo pipefail

# PATH_RE is the shape of every accepted path: plain names joined by single slashes, no `.` or `..` component.
PATH_RE='^[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)*$'

die() {
  echo "::error::$*" >&2
  exit 1
}

valid_path() {
  [[ $1 =~ $PATH_RE ]] && [[ /$1/ != */../* ]] && [[ /$1/ != */./* ]]
}

# allowed <path> succeeds when the path is one of the allowed paths or lies under one of them.
allowed() {
  local path=$1 root
  for root in "${ROOTS[@]}"; do
    if [ "$path" = "$root" ] || [[ $path == "$root"/* ]]; then
      return 0
    fi
  done
  return 1
}

# ancestor <path> succeeds when the path is a directory above one of the allowed paths.
ancestor() {
  local path=$1 root
  for root in "${ROOTS[@]}"; do
    if [[ $root == "$path"/* ]]; then
      return 0
    fi
  done
  return 1
}

main() {
  [ "$#" -ge 3 ] || die "usage: $0 <artifact-dir> <checkout-dir> <allowed-path>..."
  local src=${1%/} dest=${2%/} entry rel root
  shift 2
  ROOTS=("$@")

  for root in "${ROOTS[@]}"; do
    valid_path "$root" || die "invalid allowed path '${root}'"
  done
  if [ ! -d "$src" ] || [ -L "$src" ]; then die "artifact directory ${src} is missing"; fi
  if [ ! -d "$dest" ] || [ -L "$dest" ]; then die "checkout ${dest} is missing"; fi

  # Validate the whole artifact first, so a bad entry never leaves a half-applied checkout.
  local files=()
  while IFS= read -r -d '' entry; do
    rel=${entry#"$src"/}
    valid_path "$rel" || die "artifact entry has an unexpected name"
    if [ -L "$entry" ]; then
      die "artifact entry ${rel} is a symlink"
    elif [ -d "$entry" ]; then
      allowed "$rel" || ancestor "$rel" || die "artifact entry ${rel} is outside the allowed paths"
    elif [ -f "$entry" ]; then
      allowed "$rel" || die "artifact entry ${rel} is outside the allowed paths"
      files+=("$rel")
    else
      die "artifact entry ${rel} is not a regular file"
    fi
  done < <(find "$src" -mindepth 1 -print0)

  for root in "${ROOTS[@]}"; do
    [ -e "$src/$root" ] || die "the artifact lacks ${root}"
    # Every directory on the way to an allowed path must be a real directory in the checkout, so a copy cannot be
    # redirected outside it.
    rel=$root
    while [ "$rel" != . ]; do
      [ ! -L "$dest/$rel" ] || die "${rel} is a symlink in the checkout"
      rel=$(dirname "$rel")
    done
  done

  for root in "${ROOTS[@]}"; do
    rm -rf -- "${dest:?}/$root"
  done
  for rel in "${files[@]}"; do
    mkdir -p -- "$dest/$(dirname "$rel")"
    cp -- "$src/$rel" "$dest/$rel"
    chmod 644 -- "$dest/$rel"
  done
  echo "Applied ${#files[@]} files under: ${ROOTS[*]}"
}

main "$@"
