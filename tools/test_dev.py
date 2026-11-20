"""Unit tests for profile isolation, workspace readiness, and launcher locking."""

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


class DevProfileTests(unittest.TestCase):
    def test_branch_key_matches_legacy_profile_layout(self) -> None:
        self.assertEqual(
            dev.branch_key("main"),
            hashlib.sha256(b"main").hexdigest()[:20],
        )

    def test_detached_head_uses_full_commit_sha(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=repo, check=True)
            subprocess.run(["git", "config", "user.email", "t@example.invalid"], cwd=repo, check=True)
            (repo / "a.txt").write_text("hi\n", encoding="utf-8")
            subprocess.run(["git", "add", "a.txt"], cwd=repo, check=True)
            subprocess.run(["git", "commit", "-q", "-m", "init"], cwd=repo, check=True)
            full_sha = subprocess.check_output(
                ["git", "rev-parse", "HEAD"], cwd=repo, text=True
            ).strip()
            subprocess.run(["git", "checkout", "-q", "--detach", full_sha], cwd=repo, check=True)
            self.assertEqual(dev.current_branch(repo), f"detached-{full_sha}")
            self.assertEqual(
                dev.branch_key(dev.current_branch(repo)),
                hashlib.sha256(f"detached-{full_sha}".encode()).hexdigest()[:20],
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

    def test_resolve_profile_rejects_symlink_branch_dir(self) -> None:
        if os.name == "nt":
            self.skipTest("POSIX symlink test")
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp) / ".inferyard-dev"
            branch_b = state / "profiles" / dev.branch_key("branch-b")
            branch_b.mkdir(parents=True)
            branch_a = state / "profiles" / dev.branch_key("branch-a")
            branch_a.symlink_to(branch_b)
            with self.assertRaises(dev.DevError):
                dev.resolve_profile(state, "branch-a", rebuild=False)
            with self.assertRaises(dev.DevError):
                dev.resolve_profile(state, "branch-a", rebuild=True)

    def test_resolve_profile_rejects_junction_branch_dir(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp) / ".inferyard-dev"
            branch_b = state / "profiles" / dev.branch_key("branch-b")
            branch_b.mkdir(parents=True)
            branch_a = state / "profiles" / dev.branch_key("branch-a")
            if os.name == "nt":
                subprocess.run(
                    ["cmd", "/c", "mklink", "/J", str(branch_a), str(branch_b)],
                    check=True,
                    capture_output=True,
                )
                with self.assertRaises(dev.DevError):
                    dev.resolve_profile(state, "branch-a", rebuild=False)
                with self.assertRaises(dev.DevError):
                    dev.resolve_profile(state, "branch-a", rebuild=True)
            else:
                branch_a.mkdir(parents=True)
                target_junction = branch_a.resolve()
                orig = Path.is_junction

                def fake_junction(self: Path) -> bool:
                    return self.resolve() == target_junction or orig(self)

                with mock.patch.object(Path, "is_junction", fake_junction):
                    with self.assertRaises(dev.DevError):
                        dev.resolve_profile(state, "branch-a", rebuild=False)
                    with self.assertRaises(dev.DevError):
                        dev.resolve_profile(state, "branch-a", rebuild=True)

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

    def test_launcher_lock_rejects_concurrent_holder(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            with dev.launcher_lock(state):
                with self.assertRaises(dev.DevError):
                    with dev.launcher_lock(state):
                        pass

    def test_develop_rejects_symlink_state_root(self) -> None:
        if os.name == "nt":
            self.skipTest("POSIX symlink test")
        with tempfile.TemporaryDirectory() as tmp:
            root_a = Path(tmp) / "checkout-a"
            root_b = Path(tmp) / "checkout-b"
            (root_b / ".inferyard-dev").mkdir(parents=True)
            root_a.mkdir()
            (root_a / ".inferyard-dev").symlink_to(root_b / ".inferyard-dev")
            with self.assertRaises(dev.DevError):
                dev.develop(root_a, "stop")


if __name__ == "__main__":
    unittest.main()
