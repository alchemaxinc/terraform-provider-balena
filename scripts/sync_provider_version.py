#!/usr/bin/env python3
"""Keep the balena provider version constraint in sync across files.

The provider registry source and version constraint for `alchemaxinc/balena`
is duplicated in README.md and examples/provider/provider.tf (the latter also
feeds docs/index.md via `tfplugindocs`). A released major version should be
reflected as a `~> N` constraint in both places.

Usage:
  sync_provider_version.py set <version>   Rewrite the "~> N" constraints to
                                            the major of <version> (for
                                            example, "2.0.0" -> "~> 2").
  sync_provider_version.py check           Fail if the files disagree, or if
                                            either uses anything other than a
                                            plain "~> N" constraint.
"""

import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
TARGET_FILES = (
    REPO_ROOT / "README.md",
    REPO_ROOT / "examples" / "provider" / "provider.tf",
)

# Matches the balena provider block's source and version lines together, so
# the version constraint can only be found and rewritten when it immediately
# follows the expected source line.
BLOCK_PATTERN = re.compile(r'(source\s*=\s*"alchemaxinc/balena"\s*\n\s*version\s*=\s*")([^"]*)(")')

MAJOR_ONLY_PATTERN = re.compile(r"^~>\s*(\d+)$")

VERSION_PATTERN = re.compile(r"v?(\d+)\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?")


def _read_constraint(path):
    """Return (text, constraint) for path, or raise if the block is not unique."""
    text = path.read_text(encoding="utf-8")
    matches = BLOCK_PATTERN.findall(text)
    if len(matches) != 1:
        raise SystemExit(
            f"{path}: expected exactly one 'alchemaxinc/balena' provider "
            f"block with a version constraint, found {len(matches)}."
        )
    return text, matches[0][1]


def _require_major_only(path, constraint):
    match = MAJOR_ONLY_PATTERN.match(constraint)
    if not match:
        raise SystemExit(
            f"{path}: version constraint '{constraint}' is not a plain "
            "'~> N' major-version pin. Update this script or the file by "
            "hand."
        )
    return match.group(1)


def cmd_check():
    """Fail if the target files disagree on the provider version constraint."""
    constraints = {}
    for path in TARGET_FILES:
        _, constraint = _read_constraint(path)
        _require_major_only(path, constraint)
        constraints[path] = constraint

    unique = set(constraints.values())
    if len(unique) != 1:
        details = "\n".join(f"  {p}: {c}" for p, c in constraints.items())
        raise SystemExit("Provider version constraints are out of sync:\n" + details)

    print(f"Provider version constraints are in sync at {unique.pop()!r}.")


def cmd_set(version):
    """Rewrite the provider version constraint to the major of version."""
    match = VERSION_PATTERN.fullmatch(version)
    if not match:
        raise SystemExit(f"'{version}' is not a semantic version (expected X.Y.Z).")
    new_constraint = f"~> {match.group(1)}"

    for path in TARGET_FILES:
        text, current_constraint = _read_constraint(path)
        _require_major_only(path, current_constraint)

        if current_constraint == new_constraint:
            print(f"{path}: already pinned to {new_constraint!r}, no change.")
            continue

        updated_text, count = BLOCK_PATTERN.subn(
            lambda m: m.group(1) + new_constraint + m.group(3), text
        )
        if count != 1:
            raise SystemExit(f"{path}: expected exactly one replacement, made {count}.")
        path.write_text(updated_text, encoding="utf-8")
        print(f"{path}: updated {current_constraint!r} -> {new_constraint!r}.")


def main(argv):
    if len(argv) < 2:
        print(__doc__)
        return 2

    command = argv[1]
    if command == "check":
        cmd_check()
        return 0
    if command == "set":
        if len(argv) != 3:
            print(__doc__)
            return 2
        cmd_set(argv[2])
        return 0

    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
