#!/usr/bin/env bash
# diff-sbvr.sh — Extracts a structured summary of changes between two SBVR files.
#
# Usage: ./schema/diff-sbvr.sh <old-file> <new-file>
#
# Outputs a human-readable summary of added/removed Terms and Fact types,
# suitable for inclusion in a PR body or Copilot prompt.

set -euo pipefail

OLD="${1:?Usage: $0 <old-sbvr> <new-sbvr>}"
NEW="${2:?Usage: $0 <old-sbvr> <new-sbvr>}"

extract_terms() {
  grep -n '^Term:' "$1" | sed 's/^[0-9]*://' | sort
}

extract_facts() {
  grep -n 'Fact type:' "$1" | sed 's/^[0-9]*://' | sed 's/^[[:space:]]*//' | sort
}

extract_necessities() {
  grep -n 'Necessity:' "$1" | sed 's/^[0-9]*://' | sed 's/^[[:space:]]*//' | sort
}

has_changes=0

echo "## SBVR Schema Diff"
echo ""

# --- Terms ---
old_terms=$(extract_terms "$OLD")
new_terms=$(extract_terms "$NEW")

added_terms=$(comm -13 <(echo "$old_terms") <(echo "$new_terms") || true)
removed_terms=$(comm -23 <(echo "$old_terms") <(echo "$new_terms") || true)

if [[ -n "$added_terms" ]]; then
  has_changes=1
  echo "### Added Terms (entities/fields)"
  echo '```'
  echo "$added_terms"
  echo '```'
  echo ""
fi

if [[ -n "$removed_terms" ]]; then
  has_changes=1
  echo "### Removed Terms (entities/fields)"
  echo '```'
  echo "$removed_terms"
  echo '```'
  echo ""
fi

# --- Fact types ---
old_facts=$(extract_facts "$OLD")
new_facts=$(extract_facts "$NEW")

added_facts=$(comm -13 <(echo "$old_facts") <(echo "$new_facts") || true)
removed_facts=$(comm -23 <(echo "$old_facts") <(echo "$new_facts") || true)

if [[ -n "$added_facts" ]]; then
  has_changes=1
  echo "### Added Fact Types (relationships/fields)"
  echo '```'
  echo "$added_facts"
  echo '```'
  echo ""
fi

if [[ -n "$removed_facts" ]]; then
  has_changes=1
  echo "### Removed Fact Types (relationships/fields)"
  echo '```'
  echo "$removed_facts"
  echo '```'
  echo ""
fi

# --- Necessities ---
old_necs=$(extract_necessities "$OLD")
new_necs=$(extract_necessities "$NEW")

added_necs=$(comm -13 <(echo "$old_necs") <(echo "$new_necs") || true)
removed_necs=$(comm -23 <(echo "$old_necs") <(echo "$new_necs") || true)

if [[ -n "$added_necs" ]]; then
  has_changes=1
  echo "### Added Constraints (Necessity)"
  echo '```'
  echo "$added_necs"
  echo '```'
  echo ""
fi

if [[ -n "$removed_necs" ]]; then
  has_changes=1
  echo "### Removed Constraints (Necessity)"
  echo '```'
  echo "$removed_necs"
  echo '```'
  echo ""
fi

if [[ "$has_changes" -eq 0 ]]; then
  echo "_No structural SBVR changes detected (Terms, Fact types, Necessities are identical)._"
fi

exit 0
