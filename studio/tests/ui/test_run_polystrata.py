"""Check local-only PolyStrata real UI runner staging and isolation without launching a window."""

from __future__ import annotations

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
from unittest.mock import patch

import run_polystrata


class TestPolystrataRealRunner(unittest.TestCase):
    def make_host_assets(self, root: Path) -> tuple[Path, Path, Path, Path, Path]:
        app = root / "package/InferYard Studio.app"
        app.mkdir(parents=True)
        cli = root / "cli/inferyard"
        cli.parent.mkdir(parents=True)
        cli.write_text("#!/bin/sh\n", encoding="utf-8")

        work_dir = root / "host_inferyard/engines/polystrata"
        data_dir = root / "host_inferyard/models/polystrata-data"
        (data_dir / "packs").mkdir(parents=True)
        (data_dir / "mtp").mkdir(parents=True)
        work_dir.mkdir(parents=True)

        small_pack = data_dir / "packs/small.pack"
        large_pack = data_dir / "packs/large.pack"
        small_pack.write_bytes(b"s" * 1024)
        large_pack.write_bytes(b"l" * 8192)
        (data_dir / "mtp/mtp-q2_0.gguf").write_bytes(b"m" * 512)

        inst_path = work_dir / "installation.json"
        inst_path.write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "family": "polystrata",
                    "working_directory": str(work_dir),
                    "data_dir": str(data_dir),
                }
            ),
            encoding="utf-8",
        )
        (work_dir / "strata-small.json").write_text(
            json.dumps(
                {
                    "display_name": "Qwen3.8-Small (Q2_0)",
                    "exe": "python3",
                    "args": ["serve/server.py", "--pack", str(small_pack)],
                }
            ),
            encoding="utf-8",
        )
        (work_dir / "strata-large.json").write_text(
            json.dumps(
                {
                    "display_name": "Qwen3.8-Large (Q8_0)",
                    "exe": "python3",
                    "args": ["serve/server.py", "--pack", str(large_pack)],
                }
            ),
            encoding="utf-8",
        )
        return app, cli, inst_path, work_dir, data_dir

    def invoke(
        self,
        argv: list[str],
        *,
        xcode_exit: int = 0,
        log_text: str = run_polystrata.PASS_MARKER,
        write_report: bool = True,
    ) -> tuple[int, list[str], dict[str, str]]:
        captured_xcode_cmd: list[str] = []
        captured_xcode_env: dict[str, str] = {}

        def fake_run(
            command: list[str],
            stdout: TextIO | None = None,
            **kwargs: object,
        ) -> subprocess.CompletedProcess[str]:
            if command and command[0] in ("/bin/cp", "cp"):
                return real_run(command, **kwargs)  # type: ignore[arg-type]
            if len(command) >= 4 and command[1] == "init" and command[2] == "--out":
                ws = Path(command[3])
                (ws / "models").mkdir(parents=True, exist_ok=True)
                (ws / "engines").mkdir(parents=True, exist_ok=True)
                (ws / "results").mkdir(parents=True, exist_ok=True)
                (ws / "reports").mkdir(parents=True, exist_ok=True)
                return subprocess.CompletedProcess(command, 0, "", "")
            if command and command[0] == "xcodebuild":
                captured_xcode_cmd.extend(command)
                env = kwargs.get("env")
                if isinstance(env, dict):
                    captured_xcode_env.update(env)
                if stdout is not None:
                    stdout.write(log_text)
                if write_report and isinstance(env, dict):
                    home = Path(env["TEST_RUNNER_INFERYARD_POLYSTRATA_UI_HOME"])
                    settings = json.loads(
                        (home / "Library/Application Support/InferYard Studio/settings.json").read_text(
                            encoding="utf-8"
                        )
                    )
                    report_dir = Path(settings["workspace_root"]) / "reports/summary-001"
                    report_dir.mkdir(parents=True, exist_ok=True)
                    (report_dir / "studio.json").write_text(
                        json.dumps({"schema_version": 3}), encoding="utf-8"
                    )
                return subprocess.CompletedProcess(command, xcode_exit, "", "")
            if command and command[0] == "ps":
                return subprocess.CompletedProcess(command, 0, "", "")
            raise AssertionError(f"unexpected subprocess command: {command}")

        real_run = subprocess.run
        with (
            patch.object(sys, "argv", argv),
            patch.object(run_polystrata.sys, "platform", "darwin"),
            patch.object(run_polystrata, "require_unlocked_desktop"),
            patch.object(run_polystrata.subprocess, "run", side_effect=fake_run),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            code = run_polystrata.main()
        return code, captured_xcode_cmd, captured_xcode_env

    def test_runner_stages_smallest_local_model_isolates_home_and_preserves_host(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, cli, inst_path, work_dir, data_dir = self.make_host_assets(root)
            output = root / "output"
            code, xcode_cmd, xcode_env = self.invoke([
                "run_polystrata.py",
                "--app", str(app),
                "--cli", str(cli),
                "--installation", str(inst_path),
                "--output-root", str(output),
                "--keep-workspace",
            ])
            self.assertEqual(code, 0)
            self.assertIn("-only-testing:PreviewUITests/PolystrataRealUITests", xcode_cmd)
            self.assertNotIn("--debug-ui", xcode_cmd)
            self.assertEqual(
                xcode_env["TEST_RUNNER_INFERYARD_POLYSTRATA_UI_MODEL"], "Qwen3.8-Small (Q2_0)"
            )

            home = Path(xcode_env["TEST_RUNNER_INFERYARD_POLYSTRATA_UI_HOME"])
            self.assertTrue(home.is_relative_to(output.resolve()))
            settings = json.loads(
                (home / "Library/Application Support/InferYard Studio/settings.json").read_text(
                    encoding="utf-8"
                )
            )
            self.assertFalse(settings["keep_services_on_quit"])
            workspace = Path(settings["workspace_root"])
            self.assertTrue(workspace.is_relative_to(output.resolve()))
            staged_inst = Path(settings["engine_installations"][0]["path"])
            self.assertTrue(staged_inst.is_relative_to(workspace))
            self.assertTrue((workspace / "engines/polystrata/strata-small.json").is_file())
            self.assertFalse((workspace / "engines/polystrata/strata-large.json").exists())
            rewritten = (workspace / "engines/polystrata/strata-small.json").read_text(encoding="utf-8")
            self.assertNotIn(str(data_dir), rewritten)
            self.assertIn(str(workspace / "models/polystrata-data"), rewritten)

            # Host PolyStrata installation and both model configs remain untouched.
            self.assertTrue((work_dir / "strata-large.json").is_file())
            self.assertIn(str(data_dir), (work_dir / "strata-small.json").read_text(encoding="utf-8"))

            runs = list(output.iterdir())
            self.assertEqual(len(runs), 1)
            manifest = json.loads((runs[0] / "run.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["suite"], "polystrata-real")
            self.assertEqual(manifest["model"], "Qwen3.8-Small (Q2_0)")
            self.assertTrue((runs[0] / "studio.json").is_file())

    def test_missing_pass_marker_or_studio_report_fails(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, cli, inst_path, _, _ = self.make_host_assets(root)
            no_marker_code, _, _ = self.invoke(
                [
                    "run_polystrata.py",
                    "--app", str(app),
                    "--cli", str(cli),
                    "--installation", str(inst_path),
                    "--output-root", str(root / "out1"),
                ],
                log_text="",
            )
            self.assertEqual(no_marker_code, 65)

            no_report_code, _, _ = self.invoke(
                [
                    "run_polystrata.py",
                    "--app", str(app),
                    "--cli", str(cli),
                    "--installation", str(inst_path),
                    "--output-root", str(root / "out2"),
                ],
                write_report=False,
            )
            self.assertEqual(no_report_code, 66)


_TEST_METHODS = (
    TestPolystrataRealRunner.test_runner_stages_smallest_local_model_isolates_home_and_preserves_host,
    TestPolystrataRealRunner.test_missing_pass_marker_or_studio_report_fails,
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
