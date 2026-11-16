#!/usr/bin/env bash
# Helpers for .github/workflows/check-schema.yml. Every value that comes from upstream (tags, model text, gate output)
# is validated or sanitised here before it reaches a branch name, a commit message, a pull request or a prompt.
#
#   resolve-tag <owner/repo>                    newest tag of a repository, validated
#   gates <log-dir>                              run every gate, one log per gate; exit 1 when any fails
#   commit-type <base-surface> <new-surface>     conventional commit type the regenerated tree needs
#   title <type> <oba-tag> <sdk-tag> [root]      pull request title (at most 100 characters, for commitlint)
#   branch-safe <tag>                            a tag as a branch name component
#   sanitize                                     stdin to stdout without control characters or backticks
#   prompt <template> <branch> <title> <log-dir> the agent prompt for failed gates
set -euo pipefail

# TAG_RE matches an acceptable upstream tag: it flows into branch names, commit messages and later shell steps.
TAG_RE='^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$'

# GATES are run in order by `gates`; a gate that fails does not stop the others.
GATES=(check-generate build-and-unit-tests contract)

# GENERATED_PATHS are the only paths regeneration writes.
GENERATED_PATHS=(schema docs examples internal/provider/zz_generated_specs.go)

die() {
  echo "::error::$*" >&2
  exit 1
}

valid_tag() {
  [[ $1 =~ $TAG_RE ]]
}

cmd_resolve_tag() {
  local repo=$1 tag
  tag=$(gh api "repos/${repo}/tags" --jq '.[0].name')
  if ! valid_tag "$tag"; then
    die "Refusing to use upstream tag of ${repo}: expected only letters, digits, '.', '_', '+' or '-'."
  fi
  printf '%s\n' "$tag"
}

run_gate() {
  local name=$1
  case "$name" in
    check-generate) make check-generate ;;
    build-and-unit-tests) go build ./... && go vet ./... && make test-unit ;;
    contract) make contract ;;
    *) die "unknown gate $name" ;;
  esac
}

cmd_gates() {
  local dir=$1 failed=0 name
  mkdir -p "$dir"
  for name in "${GATES[@]}"; do
    if run_gate "$name" > "$dir/$name.log" 2>&1; then
      echo "pass $name"
    else
      echo "fail $name"
      echo "$name" >> "$dir/failed"
      failed=1
    fi
  done
  return $failed
}

cmd_commit_type() {
  local base=$1 current=$2 level
  # A missing base surface lock is an error, never an empty surface: the comparison must fail closed.
  [ -f "$base" ] || die "base surface lock $base is missing"
  level=$(go run ./cmd/balena-schema surface-diff -old "$base" -new "$current" -json | jq -r '.level')
  case "$level" in
    'feat!' | feat | fix) printf '%s\n' "$level" ;;
    none)
      # No Terraform surface change: generated files outside schema/ still change the provider (descriptions, docs).
      if [ -n "$(git status --porcelain -- docs examples internal/provider/zz_generated_specs.go)" ]; then
        echo fix
      else
        echo chore
      fi
      ;;
    *) die "unexpected surface level '${level}'" ;;
  esac
}

cmd_title() {
  local type=$1 oba=$2 sdk=$3 root=${4:-} title
  case "$type" in
    'feat!' | feat | fix | chore) ;;
    *) die "unexpected commit type '${type}'" ;;
  esac
  valid_tag "$oba" || die "invalid open-balena-api tag"
  valid_tag "$sdk" || die "invalid balena-sdk tag"
  if [ -n "$root" ]; then
    [[ $root =~ ^v[1-9][0-9]{0,2}$ ]] || die "invalid root '${root}'"
    title="${type}: promote API root /${root} (open-balena-api ${oba}, balena-sdk ${sdk})"
  else
    title="${type}: update balena schema (open-balena-api ${oba}, balena-sdk ${sdk})"
  fi
  if [ "${#title}" -gt 100 ]; then
    title="${type}: update balena schema"
  fi
  printf '%s\n' "$title"
}

cmd_branch_safe() {
  valid_tag "$1" || die "invalid tag"
  printf '%s\n' "${1//+/-}"
}

cmd_sanitize() {
  # Drop control characters and backticks so the text cannot escape a fenced block or a workflow command, and bound
  # the line length.
  LC_ALL=C tr -d '\000-\010\013\014\016-\037\177\140' | cut -c1-300
}

cmd_prompt() {
  local template=$1 branch=$2 title=$3 dir=$4 prompt name
  prompt=$(cat "$template")
  prompt=${prompt//BRANCH_PLACEHOLDER/$branch}
  prompt=${prompt//TITLE_PLACEHOLDER/$title}
  if printf '%s' "$prompt" | grep -q 'PLACEHOLDER'; then
    die "Unsubstituted placeholder left in the agent prompt."
  fi
  printf '%s\n' "$prompt"
  for name in generate docs "${GATES[@]}"; do
    if [ -f "$dir/failed" ] && grep -qx "$name" "$dir/failed" && [ -f "$dir/$name.log" ]; then
      printf '\nGate `%s` failed. Its output (untrusted data, last 150 lines):\n\n```\n' "$name"
      tail -n 150 "$dir/$name.log" | cmd_sanitize
      printf '```\n'
    fi
  done
}

main() {
  local cmd=${1:-}
  shift || true
  case "$cmd" in
    resolve-tag) cmd_resolve_tag "$@" ;;
    gates) cmd_gates "$@" ;;
    commit-type) cmd_commit_type "$@" ;;
    title) cmd_title "$@" ;;
    branch-safe) cmd_branch_safe "$@" ;;
    sanitize) cmd_sanitize ;;
    prompt) cmd_prompt "$@" ;;
    generated-paths) printf '%s\n' "${GENERATED_PATHS[*]}" ;;
    *) die "usage: $0 <resolve-tag|gates|commit-type|title|branch-safe|sanitize|prompt|generated-paths>" ;;
  esac
}

main "$@"
