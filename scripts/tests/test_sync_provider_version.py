#!/usr/bin/env python3
"""Unit tests for scripts/sync_provider_version.py."""

import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import sync_provider_version as module  # noqa: E402

SAMPLE = """terraform {
  required_providers {
    balena = {
      source  = "alchemaxinc/balena"
      version = "~> 1"
    }
  }
}
"""


class TestSyncProviderVersion(unittest.TestCase):
    def _write(self, tmp, name, content):
        path = Path(tmp) / name
        path.write_text(content, encoding="utf-8")
        return path

    def test_set_updates_major_version(self):
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", SAMPLE)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                module.cmd_set("2.0.0")
            self.assertIn('version = "~> 2"', readme.read_text(encoding="utf-8"))
            self.assertIn('version = "~> 2"', example.read_text(encoding="utf-8"))

    def test_set_accepts_leading_v(self):
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", SAMPLE)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                module.cmd_set("v3.1.4")
            self.assertIn('version = "~> 3"', readme.read_text(encoding="utf-8"))

    def test_set_is_idempotent_for_same_major(self):
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", SAMPLE)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                module.cmd_set("1.5.2")
            self.assertEqual(readme.read_text(encoding="utf-8"), SAMPLE)
            self.assertEqual(example.read_text(encoding="utf-8"), SAMPLE)

    def test_set_rejects_malformed_version(self):
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", SAMPLE)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                with self.assertRaises(SystemExit):
                    module.cmd_set("not-a-version")

    def test_set_rejects_non_major_only_constraint(self):
        odd = SAMPLE.replace('version = "~> 1"', 'version = ">= 1.0, < 2.0"')
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", odd)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                with self.assertRaises(SystemExit):
                    module.cmd_set("2.0.0")

    def test_set_rejects_missing_block(self):
        no_block = "no provider block here\n"
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", no_block)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                with self.assertRaises(SystemExit):
                    module.cmd_set("2.0.0")

    def test_set_rejects_duplicate_block(self):
        duplicated = SAMPLE + SAMPLE
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", duplicated)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                with self.assertRaises(SystemExit):
                    module.cmd_set("2.0.0")

    def test_check_passes_when_in_sync(self):
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", SAMPLE)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                module.cmd_check()  # should not raise

    def test_check_fails_when_out_of_sync(self):
        mismatched = SAMPLE.replace('version = "~> 1"', 'version = "~> 2"')
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", SAMPLE)
            example = self._write(tmp, "provider.tf", mismatched)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                with self.assertRaises(SystemExit):
                    module.cmd_check()

    def test_check_fails_on_non_major_only_constraint(self):
        odd = SAMPLE.replace('version = "~> 1"', 'version = "~> 1.0"')
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", odd)
            example = self._write(tmp, "provider.tf", odd)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                with self.assertRaises(SystemExit):
                    module.cmd_check()

    def test_main_set_invokes_cmd_set(self):
        with tempfile.TemporaryDirectory() as tmp:
            readme = self._write(tmp, "README.md", SAMPLE)
            example = self._write(tmp, "provider.tf", SAMPLE)
            with mock.patch.object(module, "TARGET_FILES", (readme, example)):
                exit_code = module.main(["sync_provider_version.py", "set", "2.0.0"])
            self.assertEqual(exit_code, 0)
            self.assertIn('version = "~> 2"', readme.read_text(encoding="utf-8"))

    def test_main_unknown_command_returns_usage_code(self):
        exit_code = module.main(["sync_provider_version.py", "bogus"])
        self.assertEqual(exit_code, 2)


if __name__ == "__main__":
    unittest.main()
