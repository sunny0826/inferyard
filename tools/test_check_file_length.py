"""Unit tests for incremental 500-line code file gate."""

from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from check_file_length import check_repo, evaluate_file_growth  # noqa: E402


class CheckFileLengthTests(unittest.TestCase):
    def test_evaluate_file_growth_boundaries(self) -> None:
        self.assertIsNone(evaluate_file_growth("a.py", 500, None, max_lines=500))
        self.assertIn("new file", evaluate_file_growth("a.py", 501, None, max_lines=500) or "")
        self.assertIn("crossed", evaluate_file_growth("a.py", 501, 499, max_lines=500) or "")
        self.assertIsNone(evaluate_file_growth("legacy.go", 540, 540, max_lines=500))
        self.assertIsNone(evaluate_file_growth("legacy.go", 520, 540, max_lines=500))
        self.assertIn("grew", evaluate_file_growth("legacy.go", 541, 540, max_lines=500) or "")

    def test_git_repo_ignores_configs_and_catches_code_growth(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.email", "ci@example.com"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.name", "CI"], cwd=root, check=True)

            (root / "legacy.py").write_text("x = 1\n" * 510, encoding="utf-8")
            (root / "small.go").write_text("package p\n" * 10, encoding="utf-8")
            subprocess.run(["git", "add", "."], cwd=root, check=True)
            subprocess.run(["git", "commit", "-q", "-m", "init"], cwd=root, check=True)
            base = subprocess.run(
                ["git", "rev-parse", "HEAD"], cwd=root, check=True, text=True, capture_output=True
            ).stdout.strip()

            # Non-code config >500 lines and non-growing legacy code file pass.
            (root / "big_config.json").write_text("{}\n" * 600, encoding="utf-8")
            (root / "legacy.py").write_text("x = 2\n" * 510, encoding="utf-8")
            self.assertEqual(check_repo(root, base, max_lines=500), [])

            # Growing legacy file and oversized new code file fail.
            (root / "legacy.py").write_text("x = 2\n" * 511, encoding="utf-8")
            (root / "new_big.py").write_text("y = 1\n" * 505, encoding="utf-8")
            errs = check_repo(root, base, max_lines=500)
            self.assertEqual(len(errs), 2)


if __name__ == "__main__":
    unittest.main()
