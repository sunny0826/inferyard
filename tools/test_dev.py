"""Unit tests for the lightweight development launcher."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import dev
import dev_process


class DevLauncherTests(unittest.TestCase):
    def test_branch_key_matches_legacy_profile_layout(self) -> None:
        self.assertEqual(
            dev.branch_key("main"),
            hashlib.sha256(b"main").hexdigest()[:20],
        )

    def test_resolve_profile_rejects_foreign_checkout_pointer(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp) / "checkout-b" / ".inferyard-dev"
            foreign = Path(tmp) / "checkout-a" / "profile-old"
            foreign.mkdir(parents=True)
            branch_dir = state / "profiles" / dev.branch_key("main")
            branch_dir.mkdir(parents=True)
            (branch_dir / "current.json").write_text(
                json.dumps({"profile_root": str(foreign)}), encoding="utf-8"
            )
            profile, _ = dev.resolve_profile(state, "main", rebuild=False)
            self.assertNotEqual(profile, foreign.resolve())
            self.assertTrue(profile.is_relative_to(branch_dir.resolve()))

    def test_resolve_profile_reuses_valid_and_rebuild_preserves_history(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            first, pointer = dev.resolve_profile(state, "main", rebuild=False)
            dev.atomic_write_json(pointer, {"profile_root": str(first)})
            marker = first / "old-run.txt"
            marker.write_text("keep", encoding="utf-8")

            reused, _ = dev.resolve_profile(state, "main", rebuild=False)
            self.assertEqual(reused, first)

            fresh, _ = dev.resolve_profile(state, "main", rebuild=True)
            self.assertNotEqual(fresh, first)
            self.assertTrue(marker.is_file())

    def test_workspace_readiness_rejects_empty_or_invalid_workspace(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            profile = Path(tmp)
            workspace = profile / "workspace"
            workspace.mkdir()
            with self.assertRaises(dev.DevError):
                dev.ensure_workspace(Path("/nonexistent/cli"), profile)
            (workspace / "preparation.json").write_text('{"kind": "other"}', encoding="utf-8")
            with self.assertRaises(dev.DevError):
                dev.ensure_workspace(Path("/nonexistent/cli"), profile)
            (workspace / "preparation.json").write_text(
                '{"kind": "community_init.v1"}', encoding="utf-8"
            )
            self.assertEqual(dev.ensure_workspace(Path("/nonexistent/cli"), profile), workspace)

    def test_legacy_active_blocks_launch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            (state / "active.json").write_text("{}", encoding="utf-8")
            with self.assertRaises(dev.DevError):
                dev.check_legacy_active(state)

    def test_stop_does_not_signal_other_process_with_same_arg(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            fake_binary = str((state / "bin" / "inferyard-studio").resolve())
            proc = subprocess.Popen(
                [sys.executable, "-c", "import time; time.sleep(30)", fake_binary]
            )
            try:
                session = state / "session.json"
                dev.atomic_write_json(
                    session,
                    {
                        "pid": proc.pid,
                        "binary": fake_binary,
                        "start_id": "different-start-id",
                        "profile": str(state / "profile"),
                    },
                )
                stopped = dev.stop_recorded(state)
                self.assertFalse(stopped)
                self.assertIsNone(proc.poll())
                self.assertFalse(session.exists())
            finally:
                proc.terminate()
                proc.wait(timeout=5)

    def test_stop_blocks_when_engine_owner_or_shutdown_failed_remains(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            profile = state / "profile"
            owner = profile / "workspace" / "logs" / "engine-owner.json"
            owner.parent.mkdir(parents=True)
            owner.write_text('{"pid": 123}', encoding="utf-8")
            session = state / "session.json"
            dev.atomic_write_json(
                session,
                {
                    "pid": 999999,
                    "binary": str(state / "bin" / "inferyard-studio"),
                    "start_id": "start-1",
                    "profile": str(profile),
                },
            )
            with mock.patch.object(dev_process, "process_identity", return_value=None):
                with self.assertRaises(dev.DevError):
                    dev.stop_recorded(state)
            self.assertTrue(session.exists())

            owner.unlink()
            status_file = state / "status.json"
            dev.atomic_write_json(status_file, {"state": "shutdown_failed", "error": "timeout"})
            dev.atomic_write_json(
                session,
                {
                    "pid": 999999,
                    "binary": str(state / "bin" / "inferyard-studio"),
                    "start_id": "start-1",
                    "profile": str(profile),
                    "status_file": str(status_file),
                },
            )
            with mock.patch.object(dev_process, "process_identity", return_value=None):
                with self.assertRaises(dev.DevError):
                    dev.stop_recorded(state)
            self.assertTrue(session.exists())

    def test_launch_studio_fails_on_early_exit_without_writing_session(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            state = root / ".inferyard-dev"
            state.mkdir()
            profile = state / "profile"
            assets = state / "assets"
            profile.mkdir()
            assets.mkdir()
            failing_bin = root / "fail.py"
            failing_bin.write_text("#!/usr/bin/env python3\nimport sys\nsys.exit(42)\n", encoding="utf-8")
            if os.name != "nt":
                failing_bin.chmod(0o755)
                with self.assertRaises(dev.DevError):
                    dev.launch_studio(
                        root, state, failing_bin, Path("/bin/echo"), profile, assets, "test"
                    )
                self.assertFalse((state / "session.json").exists())

    def test_launcher_lock_rejects_concurrent_holder(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            with dev.launcher_lock(state):
                with self.assertRaises(dev.DevError):
                    with dev.launcher_lock(state):
                        pass


if __name__ == "__main__":
    unittest.main()
