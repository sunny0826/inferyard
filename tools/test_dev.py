"""Unit tests for the lightweight development launcher."""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import dev


class DevLauncherTests(unittest.TestCase):
    def test_branch_key_is_safe_and_distinct(self) -> None:
        a = dev.branch_key("feat/foo")
        b = dev.branch_key("feat-foo")
        self.assertNotIn("/", a)
        self.assertNotEqual(a, b)

    def test_resolve_profile_reuses_and_rebuild_preserves_history(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            first, _ = dev.resolve_profile(state, "main", rebuild=False, commit_pointer=True)
            marker = first / "old-run.txt"
            marker.write_text("keep", encoding="utf-8")

            reused, _ = dev.resolve_profile(state, "main", rebuild=False, commit_pointer=True)
            self.assertEqual(reused, first)

            fresh, _ = dev.resolve_profile(state, "main", rebuild=True, commit_pointer=True)
            self.assertNotEqual(fresh, first)
            self.assertTrue(marker.is_file())

    def test_stop_recorded_cleans_stale_session(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            session = state / "session.json"
            session.write_text(json.dumps({"pid": 999999, "binary": "/nonexistent"}), encoding="utf-8")
            with mock.patch.object(dev, "_process_matches", return_value=False):
                stopped = dev.stop_recorded(state)
            self.assertFalse(stopped)
            self.assertFalse(session.exists())


if __name__ == "__main__":
    unittest.main()
