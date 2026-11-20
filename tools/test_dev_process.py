"""Unit tests for process identity, readiness handoff, shutdown cleanup, and ENOSPC recovery."""

from __future__ import annotations

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


class DevProcessTests(unittest.TestCase):
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
            dev.atomic_write_json(status_file, {"state": "ready", "pid": 999999})
            with mock.patch.object(dev_process, "process_identity", return_value=None):
                with self.assertRaises(dev.DevError):
                    dev.stop_recorded(state)
            self.assertTrue(session.exists())

            dev.atomic_write_json(
                status_file, {"state": "shutdown_failed", "pid": 999999, "error": "timeout"}
            )
            with mock.patch.object(dev_process, "process_identity", return_value=None):
                with self.assertRaises(dev.DevError):
                    dev.stop_recorded(state)
            self.assertTrue(session.exists())

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
            failing_bin.write_text(
                f"#!{sys.executable}\nimport sys\nsys.exit(42)\n", encoding="utf-8"
            )
            failing_bin.chmod(0o755)
            with self.assertRaises(dev.DevError):
                dev.launch_studio(
                    root, state, failing_bin, Path("/bin/echo"), profile, assets, "test"
                )
            self.assertFalse((state / "session.json").exists())

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
            self.assertEqual(spawned, [])
            self.assertFalse((state / "session.json").exists())

            real_write = dev_process.atomic_write_json
            writes = 0

            def fail_after_pending(path: Path, value: dict[str, object]) -> None:
                nonlocal writes
                writes += 1
                if writes >= 2:
                    raise dev.DevError("disk full")
                real_write(path, value)

            with (
                mock.patch.object(dev_process.subprocess, "Popen", side_effect=record_popen),
                mock.patch.object(dev_process, "atomic_write_json", side_effect=fail_after_pending),
            ):
                with self.assertRaises(dev.DevError):
                    dev.launch_studio(
                        root, state, slow_bin, Path("/bin/echo"), profile, assets, "test"
                    )
            self.assertEqual(len(spawned), 1)
            self.assertIsNone(dev_process.process_identity(spawned[0]))
            self.assertFalse((state / "session.json").exists())

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

    def test_initial_write_failure_preserves_shutdown_failed_and_timeout_session(self) -> None:
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
            armed = root / "armed.flag"
            fail_shutdown = root / "fail_on_term.py"
            fail_shutdown.write_text(
                f"#!{sys.executable}\n"
                "import json, os, pathlib, signal, sys, time\n"
                "status_path = sys.argv[sys.argv.index('--dev-status') + 1]\n"
                "def handle(*_):\n"
                "    with open(status_path, 'w') as f:\n"
                "        json.dump({'state': 'shutdown_failed', 'pid': os.getpid(), 'error': 'cleanup_err'}, f)\n"
                "    sys.exit(1)\n"
                "signal.signal(signal.SIGTERM, handle)\n"
                f"pathlib.Path({str(armed)!r}).write_text('ok')\n"
                "time.sleep(30)\n",
                encoding="utf-8",
            )
            fail_shutdown.chmod(0o755)
            real_write = dev_process.atomic_write_json
            attempts = 0

            def fail_once(path: Path, value: dict[str, object]) -> None:
                nonlocal attempts
                attempts += 1
                if attempts == 2:
                    deadline = dev_process.time.monotonic() + 5.0
                    while not armed.exists() and dev_process.time.monotonic() < deadline:
                        dev_process.time.sleep(0.02)
                    raise dev.DevError("transient write error")
                real_write(path, value)

            with mock.patch.object(dev_process, "atomic_write_json", side_effect=fail_once):
                with self.assertRaises(dev.DevError):
                    dev.launch_studio(
                        root, state, fail_shutdown, Path("/bin/echo"), profile, assets, "test"
                    )
            session_path = state / "session.json"
            self.assertTrue(session_path.is_file())
            session_path.unlink()

            armed.unlink(missing_ok=True)
            stubborn = root / "stubborn.py"
            stubborn.write_text(
                f"#!{sys.executable}\n"
                "import pathlib, signal, time\n"
                "signal.signal(signal.SIGTERM, signal.SIG_IGN)\n"
                f"pathlib.Path({str(armed)!r}).write_text('ok')\n"
                "time.sleep(30)\n",
                encoding="utf-8",
            )
            stubborn.chmod(0o755)
            attempts = 0
            with mock.patch.object(dev_process, "atomic_write_json", side_effect=fail_once):
                with self.assertRaises(dev.DevError):
                    dev.launch_studio(
                        root,
                        state,
                        stubborn,
                        Path("/bin/echo"),
                        profile,
                        assets,
                        "test",
                        stop_timeout=0.2,
                    )
            self.assertTrue(session_path.is_file())
            pid = json.loads(session_path.read_text(encoding="utf-8"))["pid"]
            try:
                os.kill(pid, 9)
                os.waitpid(pid, 0)
            except (OSError, ChildProcessError):
                pass
            session_path.unlink(missing_ok=True)

            armed.unlink(missing_ok=True)
            spawned_stubborn: list[int] = []
            real_popen = dev_process.subprocess.Popen

            def track_popen(*args, **kwargs):
                p = real_popen(*args, **kwargs)
                spawned_stubborn.append(p.pid)
                return p

            persist_calls = 0

            def fail_all_after_pending(path: Path, value: dict[str, object]) -> None:
                nonlocal persist_calls
                persist_calls += 1
                if persist_calls >= 2:
                    deadline = dev_process.time.monotonic() + 5.0
                    while not armed.exists() and dev_process.time.monotonic() < deadline:
                        dev_process.time.sleep(0.02)
                    raise dev.DevError("ENOSPC")
                real_write(path, value)

            with (
                mock.patch.object(dev_process.subprocess, "Popen", side_effect=track_popen),
                mock.patch.object(
                    dev_process, "atomic_write_json", side_effect=fail_all_after_pending
                ),
            ):
                with self.assertRaises(dev.DevError):
                    dev.launch_studio(
                        root,
                        state,
                        stubborn,
                        Path("/bin/echo"),
                        profile,
                        assets,
                        "test",
                        stop_timeout=0.2,
                    )
            self.assertTrue(session_path.is_file())
            self.assertEqual(json.loads(session_path.read_text(encoding="utf-8"))["state"], "pending")
            with self.assertRaises(dev.DevError):
                dev.stop_recorded(state)
            self.assertTrue(session_path.is_file())
            for pid in spawned_stubborn:
                try:
                    os.kill(pid, 9)
                    os.waitpid(pid, 0)
                except (OSError, ChildProcessError):
                    pass

    def test_interrupt_after_popen_during_log_close_does_not_drop_session(self) -> None:
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
            armed = root / "armed_close.flag"
            stubborn = root / "stubborn_close.py"
            stubborn.write_text(
                f"#!{sys.executable}\n"
                "import pathlib, signal, time\n"
                "signal.signal(signal.SIGTERM, signal.SIG_IGN)\n"
                f"pathlib.Path({str(armed)!r}).write_text('ok')\n"
                "time.sleep(30)\n",
                encoding="utf-8",
            )
            stubborn.chmod(0o755)
            spawned: list[int] = []
            real_popen = dev_process.subprocess.Popen
            real_open = Path.open

            class InterruptOnExit:
                def __init__(self, inner):
                    self._inner = inner

                def __enter__(self):
                    return self._inner.__enter__()

                def __exit__(self, *exc):
                    self._inner.__exit__(*exc)
                    deadline = dev_process.time.monotonic() + 5.0
                    while not armed.exists() and dev_process.time.monotonic() < deadline:
                        dev_process.time.sleep(0.02)
                    raise KeyboardInterrupt

            def track_popen(*args, **kwargs):
                p = real_popen(*args, **kwargs)
                spawned.append(p.pid)
                return p

            def wrapped_open(self: Path, *args, **kwargs):
                handle = real_open(self, *args, **kwargs)
                if self.suffix == ".log":
                    return InterruptOnExit(handle)
                return handle

            with (
                mock.patch.object(dev_process.subprocess, "Popen", side_effect=track_popen),
                mock.patch.object(Path, "open", wrapped_open),
            ):
                with self.assertRaises(KeyboardInterrupt):
                    dev.launch_studio(
                        root,
                        state,
                        stubborn,
                        Path("/bin/echo"),
                        profile,
                        assets,
                        "test",
                        stop_timeout=0.2,
                    )
            self.assertEqual(len(spawned), 1)
            session_path = state / "session.json"
            self.assertTrue(session_path.is_file())
            for pid in spawned:
                try:
                    os.kill(pid, 9)
                    os.waitpid(pid, 0)
                except (OSError, ChildProcessError):
                    pass


if __name__ == "__main__":
    unittest.main()
