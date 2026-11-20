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

    def test_process_identity_is_timezone_invariant(self) -> None:
        if os.name == "nt":
            self.skipTest("POSIX timezone check")
        old_tz = os.environ.get("TZ")
        try:
            os.environ["TZ"] = "UTC0"
            a = dev_process.process_identity(os.getpid())
            os.environ["TZ"] = "JST-9"
            b = dev_process.process_identity(os.getpid())
            self.assertIsNotNone(a)
            self.assertEqual(a, b)
        finally:
            if old_tz is None:
                os.environ.pop("TZ", None)
            else:
                os.environ["TZ"] = old_tz

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

    def test_stop_requires_stopped_status_and_no_engine_owner(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            profile = state / "profile"
            owner = profile / "workspace" / "logs" / "engine-owner.json"
            owner.parent.mkdir(parents=True)
            owner.write_text('{"pid": 123}', encoding="utf-8")
            status_file = state / "status.json"
            dev.atomic_write_json(status_file, {"state": "stopped", "pid": 999999})
            session = state / "session.json"
            dev.atomic_write_json(
                session,
                {
                    "state": "ready",
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

            owner.unlink()
            # Still "ready" (e.g., abnormal exit without clean shutdown) must fail and keep session.
            dev.atomic_write_json(status_file, {"state": "ready", "pid": 999999})
            with mock.patch.object(dev_process, "process_identity", return_value=None):
                with self.assertRaises(dev.DevError):
                    dev.stop_recorded(state)
            self.assertTrue(session.exists())

            # Shutdown failure must also fail and keep session.
            dev.atomic_write_json(status_file, {"state": "shutdown_failed", "pid": 999999, "error": "timeout"})
            with mock.patch.object(dev_process, "process_identity", return_value=None):
                with self.assertRaises(dev.DevError):
                    dev.stop_recorded(state)
            self.assertTrue(session.exists())

            # Matching PID "stopped" confirmation allows cleanup.
            dev.atomic_write_json(status_file, {"state": "stopped", "pid": 999999})
            with mock.patch.object(dev_process, "process_identity", return_value=None):
                dev.stop_recorded(state)
            self.assertFalse(session.exists())

    def test_launch_studio_retains_session_on_interrupt_so_stop_can_reclaim(self) -> None:
        if os.name == "nt":
            self.skipTest("POSIX signal test")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            state = root / ".inferyard-dev"
            state.mkdir()
            profile = state / "profile"
            assets = state / "assets"
            profile.mkdir()
            assets.mkdir()
            slow_bin = root / "slow.py"
            slow_bin.write_text(
                f"#!{sys.executable}\n"
                "import signal, sys, time\n"
                "signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))\n"
                "time.sleep(30)\n",
                encoding="utf-8",
            )
            slow_bin.chmod(0o755)
            calls = 0
            real_sleep = dev_process.time.sleep

            def staged_interrupt(_seconds: float) -> None:
                nonlocal calls
                calls += 1
                if calls == 1:
                    real_sleep(0.05)
                    return
                raise KeyboardInterrupt

            with mock.patch.object(dev_process.time, "sleep", side_effect=staged_interrupt):
                with self.assertRaises(KeyboardInterrupt):
                    dev.launch_studio(
                        root, state, slow_bin, Path("/bin/echo"), profile, assets, "test"
                    )
            session_path = state / "session.json"
            self.assertTrue(session_path.is_file())
            pid = json.loads(session_path.read_text(encoding="utf-8"))["pid"]
            try:
                stopped = dev.stop_recorded(state)
                self.assertTrue(stopped)
                self.assertFalse(session_path.exists())
            finally:
                try:
                    os.waitpid(pid, 0)
                except ChildProcessError:
                    pass

    def test_launch_studio_fails_on_early_exit_without_writing_session(self) -> None:
        if os.name == "nt":
            self.skipTest("POSIX executable script test")
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

    def test_launch_terminates_child_if_initial_session_write_fails(self) -> None:
        if os.name == "nt":
            self.skipTest("POSIX executable script test")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            state = root / ".inferyard-dev"
            state.mkdir()
            profile = state / "profile"
            assets = state / "assets"
            profile.mkdir()
            assets.mkdir()
            slow_bin = root / "slow.py"
            slow_bin.write_text(
                f"#!{sys.executable}\n"
                "import signal, sys, time\n"
                "signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))\n"
                "time.sleep(30)\n",
                encoding="utf-8",
            )
            slow_bin.chmod(0o755)
            spawned: list[int] = []
            real_popen = dev_process.subprocess.Popen

            def record_popen(*args, **kwargs):
                p = real_popen(*args, **kwargs)
                spawned.append(p.pid)
                return p

            with (
                mock.patch.object(dev_process.subprocess, "Popen", side_effect=record_popen),
                mock.patch.object(
                    dev_process, "atomic_write_json", side_effect=dev.DevError("disk full")
                ),
            ):
                with self.assertRaises(dev.DevError):
                    dev.launch_studio(
                        root, state, slow_bin, Path("/bin/echo"), profile, assets, "test"
                    )
            self.assertEqual(len(spawned), 1)
            self.assertIsNone(dev_process.process_identity(spawned[0]))

    def test_early_exit_with_shutdown_failed_retains_session(self) -> None:
        if os.name == "nt":
            self.skipTest("POSIX executable script test")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            state = root / ".inferyard-dev"
            state.mkdir()
            profile = state / "profile"
            assets = state / "assets"
            profile.mkdir()
            assets.mkdir()
            bad_bin = root / "bad_shutdown.py"
            bad_bin.write_text(
                f"#!{sys.executable}\n"
                "import json, os, sys\n"
                "status_path = sys.argv[sys.argv.index('--dev-status') + 1]\n"
                "with open(status_path, 'w') as f:\n"
                "    json.dump({'state': 'shutdown_failed', 'pid': os.getpid(), 'error': 'boom'}, f)\n"
                "sys.exit(42)\n",
                encoding="utf-8",
            )
            bad_bin.chmod(0o755)
            with self.assertRaises(dev.DevError):
                dev.launch_studio(
                    root, state, bad_bin, Path("/bin/echo"), profile, assets, "test"
                )
            self.assertTrue((state / "session.json").is_file())
            with self.assertRaises(dev.DevError):
                dev.stop_recorded(state)
            self.assertTrue((state / "session.json").is_file())


if __name__ == "__main__":
    unittest.main()
