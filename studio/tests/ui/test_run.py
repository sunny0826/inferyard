"""Check suite selection and failure reporting without launching a native app."""

import contextlib
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import run


class RunnerTests(unittest.TestCase):
    def invoke(self, suite_args, exit_code):
        with tempfile.TemporaryDirectory() as directory:
            argv = ["run.py", "--output-root", directory, *suite_args]
            with (
                patch.object(sys, "argv", argv),
                patch.object(sys, "platform", "darwin"),
                patch.dict(os.environ, {"TEST_RUNNER_INFERYARD_UI_SUITE": "stale"}),
                patch.object(run, "require_unlocked_desktop"),
                patch.object(
                    run.subprocess, "run", return_value=subprocess.CompletedProcess([], exit_code)
                ) as launch,
                contextlib.redirect_stdout(io.StringIO()),
            ):
                result = run.main()
            (output,) = Path(directory).iterdir()
            manifest = json.loads((output / "run.json").read_text())
            return result, launch.call_args.kwargs["env"], manifest

    def test_default_daily_overrides_inherited_suite(self):
        result, env, manifest = self.invoke([], 0)
        self.assertEqual(result, 0)
        self.assertEqual(env["TEST_RUNNER_INFERYARD_UI_SUITE"], "daily")
        self.assertEqual(manifest["suite"], "daily")
        self.assertEqual(manifest["exit_code"], 0)
        self.assertGreaterEqual(manifest["elapsed_seconds"], 0)

    def test_full_failure_is_reported_and_returned(self):
        result, env, manifest = self.invoke(["--suite", "full"], 65)
        self.assertEqual(result, 65)
        self.assertEqual(env["TEST_RUNNER_INFERYARD_UI_SUITE"], "full")
        self.assertEqual(manifest["suite"], "full")
        self.assertEqual(manifest["exit_code"], 65)

    def test_unknown_suite_fails_before_desktop_or_build(self):
        with (
            patch.object(sys, "argv", ["run.py", "--suite", "ful"]),
            patch.object(run, "require_unlocked_desktop") as desktop,
            patch.object(run.subprocess, "run") as launch,
            contextlib.redirect_stderr(io.StringIO()),
            self.assertRaises(SystemExit) as error,
        ):
            run.main()
        self.assertEqual(error.exception.code, 2)
        desktop.assert_not_called()
        launch.assert_not_called()


if __name__ == "__main__":
    unittest.main()
