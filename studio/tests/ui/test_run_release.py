"""Check production-package UI runner inputs without launching a native app."""

import contextlib
import fnmatch
import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import TextIO
from unittest.mock import Mock, patch

import run_release

RELEASE_PASS = "Test Case '-[PreviewUITests.ReleaseUITests testProductionPagesAndRealReport]' passed"


class TestReleaseRunner(unittest.TestCase):
    def make_inputs(self, root: Path) -> tuple[Path, Path, Path]:
        app = root / "package/InferYard Studio.app"
        app.mkdir(parents=True)
        report = root / "e2e/workspace/reports/report-1/studio.json"
        report.parent.mkdir(parents=True)
        report.write_text("{}")
        cli = root / "cli/inferyard"
        cli.parent.mkdir()
        cli.write_text("")
        return app, report.parents[3], cli

    def invoke(
        self, argv: list[str], exit_code: int = 0, log_text: str = RELEASE_PASS
    ) -> tuple[int, Mock]:
        def fake_run(
            command: list[str], stdout: TextIO, **_: str | int | bool | Path | dict[str, str]
        ) -> subprocess.CompletedProcess:
            stdout.write(log_text)
            return subprocess.CompletedProcess(command, exit_code)

        with (
            patch.object(sys, "argv", argv),
            patch.object(run_release.sys, "platform", "darwin"),
            patch.object(run_release, "require_unlocked_desktop"),
            patch.object(run_release.subprocess, "run", side_effect=fake_run) as launch,
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = run_release.main()
        return result, launch

    def test_release_runner_isolates_home_and_binds_e2e_report(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, e2e_root, cli = self.make_inputs(root)
            output = root / "output"
            result, launch = self.invoke([
                "run_release.py",
                "--app", str(app),
                "--e2e-root", str(e2e_root),
                "--cli", str(cli),
                "--output-root", str(output),
            ])
            self.assertEqual(result, 0)
            command = launch.call_args.args[0]
            self.assertIn("-only-testing:PreviewUITests/ReleaseUITests", command)
            self.assertNotIn("--debug-ui", command)
            env = launch.call_args.kwargs["env"]
            home = Path(env["TEST_RUNNER_INFERYARD_RELEASE_HOME"])
            self.assertTrue(home.is_relative_to(output.resolve()))
            settings = json.loads(
                (home / "Library/Application Support/InferYard Studio/settings.json").read_text()
            )
            self.assertEqual(settings["inferyard_path"], str(cli.resolve()))
            self.assertEqual(settings["workspace_root"], str((e2e_root / "workspace").resolve()))
            self.assertEqual(
                settings["results_root"], str((e2e_root / "workspace/results").resolve())
            )
            self.assertEqual(settings["models_root"], str((e2e_root / "models").resolve()))
            self.assertEqual(env["TEST_RUNNER_INFERYARD_RELEASE_APP"], str(app.resolve()))
            derived_index = command.index("-derivedDataPath") + 1
            self.assertEqual(
                Path(command[derived_index]), output.resolve().parent / "ui-derived-data"
            )
            entries = list(output.iterdir())
            self.assertEqual(len(entries), 1)
            self.assertTrue(entries[0].name.startswith("run-"))
            run = entries[0] / "run.json"
            self.assertEqual(json.loads(run.read_text())["suite"], "release")
            self.assertFalse((entries[0] / "DerivedData").exists())

    def test_missing_pass_receipt_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, e2e_root, cli = self.make_inputs(root)
            result, _ = self.invoke([
                "run_release.py",
                "--app", str(app),
                "--e2e-root", str(e2e_root),
                "--cli", str(cli),
                "--output-root", str(root / "output"),
            ], log_text="")
            self.assertEqual(result, 65)


_TEST_METHODS = (
    TestReleaseRunner.test_release_runner_isolates_home_and_binds_e2e_report,
    TestReleaseRunner.test_missing_pass_receipt_is_rejected,
)


def load_tests(
    loader: unittest.TestLoader,
    tests: unittest.TestSuite,
    pattern: str | None,
) -> unittest.TestSuite:
    for method in _TEST_METHODS:
        if pattern is None or fnmatch.fnmatch(method.__name__, pattern):
            tests.addTest(loader.loadTestsFromName(method.__qualname__, sys.modules[__name__]))
    return tests


if __name__ == "__main__":
    unittest.main()
