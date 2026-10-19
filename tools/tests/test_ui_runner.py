"""Native test entry rejects a desktop that cannot support UI evidence."""

import importlib.util
import io
import plistlib
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[2] / "studio/tests/ui/run.py"
SPEC = importlib.util.spec_from_file_location("native_ui_runner", SCRIPT)
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


class NativeUIRunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.output = Path(self.temp.name) / "artifacts"
        self.enterContext(redirect_stdout(io.StringIO()))
        self.enterContext(redirect_stderr(io.StringIO()))
        self.enterContext(
            patch.object(sys, "argv", [str(SCRIPT), "--output-root", str(self.output)])
        )
        self.enterContext(patch.object(sys, "platform", "darwin"))
        self.commands = []

    def command(self, args, **kwargs):
        self.commands.append(args[0])
        data = {"IOConsoleUsers": self.sessions}
        return subprocess.CompletedProcess(args, 0, plistlib.dumps(data), b"")

    def reject(self, sessions):
        self.sessions = sessions
        with patch.object(subprocess, "run", side_effect=self.command):
            with self.assertRaises(SystemExit) as error:
                runner.main()
        self.assertEqual(error.exception.code, 2)
        self.assertNotIn("xcodebuild", self.commands)
        self.assertFalse(self.output.exists())

    def test_locked_desktop_does_not_start_native_tests(self):
        self.reject(
            [
                {
                    "kCGSSessionOnConsoleKey": True,
                    "kCGSessionLoginDoneKey": True,
                    "CGSSessionScreenIsLocked": True,
                }
            ]
        )

    def test_missing_console_session_does_not_start_native_tests(self):
        self.reject([])

    def test_login_not_finished_does_not_start_native_tests(self):
        self.reject([{"kCGSSessionOnConsoleKey": True, "kCGSessionLoginDoneKey": False}])

    def test_probe_error_does_not_start_native_tests(self):
        with patch.object(subprocess, "run", side_effect=OSError("probe unavailable")):
            with self.assertRaises(SystemExit) as error:
                runner.main()
        self.assertEqual(error.exception.code, 2)
        self.assertFalse(self.output.exists())

    def test_malformed_desktop_state_does_not_start_native_tests(self):
        with patch.object(
            subprocess, "run", return_value=subprocess.CompletedProcess([], 0, b"invalid plist")
        ):
            with self.assertRaises(SystemExit) as error:
                runner.main()
        self.assertEqual(error.exception.code, 2)
        self.assertFalse(self.output.exists())

    def test_unlocked_desktop_runs_native_tests(self):
        self.sessions = [{"kCGSSessionOnConsoleKey": True, "kCGSessionLoginDoneKey": True}]
        with patch.object(subprocess, "run", side_effect=self.command):
            self.assertEqual(runner.main(), 0)
        self.assertIn("xcodebuild", self.commands)
        self.assertTrue(next(self.output.glob("run-*/xcodebuild.log")).is_file())


if __name__ == "__main__":
    unittest.main()
