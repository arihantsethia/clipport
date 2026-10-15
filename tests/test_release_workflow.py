"""Exercise the publication guard directly from the workflow without GitHub writes."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest


WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/release.yml"


def release(tag, *, draft=False, prerelease=False):
    return {"tag_name": tag, "draft": draft, "prerelease": prerelease}


class ReleaseGuardTests(unittest.TestCase):
    def run_guard(self, pages, tag="v0.1.2"):
        # Keep the write-enabled job self-contained; test its actual embedded code.
        source = WORKFLOW.read_text().split("python3 - <<'PY'\n", 1)[1]
        source = textwrap.dedent(source.split("\n          PY", 1)[0])
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "releases.json").write_text(json.dumps(pages))
            output = root / "env"
            result = subprocess.run(
                [sys.executable, "-c", source],
                env={**os.environ, "RUNNER_TEMP": directory,
                     "RELEASE_TAG": tag, "GITHUB_ENV": str(output)},
                capture_output=True, text=True, check=False,
            )
            return result, output.read_text() if output.exists() else ""

    def test_existing_stable_is_noop_regardless_of_order_or_page(self):
        target, newer = release("v0.1.2"), release("v0.1.3")
        for pages in ([[newer, target]], [[target, newer]], [[newer], [target]]):
            with self.subTest(pages=pages):
                result, output = self.run_guard(pages)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("nothing to change", result.stdout)
                self.assertEqual(output, "")

    def test_new_stable_is_created(self):
        for pages in ([], [[]], [[release("v0.1.1")]]):
            with self.subTest(pages=pages):
                result, output = self.run_guard(pages)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(output, "CREATE_RELEASE=true\n")

    def test_unpublished_older_version_is_rejected(self):
        result, output = self.run_guard([[release("v0.1.3")]])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("older version", result.stderr)
        self.assertEqual(output, "")

    def test_existing_draft_or_prerelease_is_rejected(self):
        for target in (release("v0.1.2", draft=True),
                       release("v0.1.2", prerelease=True)):
            with self.subTest(target=target):
                result, output = self.run_guard([[release("v0.1.3"), target]])
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("draft/prerelease", result.stderr)
                self.assertEqual(output, "")

    def test_nonstable_releases_do_not_block_new_stable(self):
        result, output = self.run_guard([[
            release("v9.0.0", draft=True),
            release("v8.0.0", prerelease=True),
            release("v7.0.0-beta.1"),
        ]])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output, "CREATE_RELEASE=true\n")

    def test_versions_are_compared_numerically(self):
        result, output = self.run_guard([[release("v0.10.0")]], tag="v0.9.0")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(output, "")


if __name__ == "__main__":
    unittest.main()
