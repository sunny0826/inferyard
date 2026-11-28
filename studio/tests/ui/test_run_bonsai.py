"""Check local-only Bonsai real UI runner discovery, CoW staging, and isolation without a window."""

from __future__ import annotations

import contextlib
import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import TextIO
from unittest.mock import patch

import bonsai_stage
import run_bonsai


def _write_exe(path: Path, version_text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(f"#!/bin/sh\necho {json.dumps(version_text)}\n", encoding="utf-8")
    path.chmod(0o755)


class TestBonsaiRealRunner(unittest.TestCase):
    def make_host_assets(self, root: Path) -> tuple[Path, Path, Path, Path, Path, Path]:
        app = root / "package/InferYard Studio.app"
        app.mkdir(parents=True)
        cli = root / "cli/inferyard"
        cli.parent.mkdir(parents=True)
        cli.write_text("#!/bin/sh\n", encoding="utf-8")

        home = root / "real_home"
        eng_dir = home / ".inferyard/engines/llama-cpp-prism-b12232-f702476"
        eng_bin = eng_dir / "llama-prism-b12232-f702476/llama-server"
        _write_exe(eng_bin, "version: 0.7.0-dev (build 12232, commit f70247610)")
        (eng_dir / "engine-prepare.json").write_text(
            json.dumps(
                {
                    "out": str(eng_dir),
                    "source_url": "https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b12232-f702476/llama-prism-b12232-f702476-bin-macos-arm64.tar.gz",
                }
            ),
            encoding="utf-8",
        )

        # Older PrismML b10743 and upstream ggml-org builds must be ignored by engine discovery.
        old_prism = home / ".inferyard/engines/llama-prism-b10743/llama-prism-b10743/llama-server"
        _write_exe(old_prism, "version: 0.6.0 (build 10743, commit adfffbe41)")
        upstream = home / ".inferyard/engines/llama-cpp-upstream/llama-server"
        _write_exe(upstream, "version: 13000 (abcdef1)")

        ptq_dir = home / ".inferyard/models/ternary-bonsai-2.5-27b-ptq1"
        pq2_dir = home / ".inferyard/models/ternary-bonsai-2.5-27b-pq2"
        ptq_dir.mkdir(parents=True)
        pq2_dir.mkdir(parents=True)
        (ptq_dir / "Ternary-Bonsai-2.5-27B-PTQ1_0.gguf").write_bytes(b"p" * 1024)
        (pq2_dir / "Ternary-Bonsai-2.5-27B-PQ2_0.gguf").write_bytes(b"q" * 4096)

        return app, cli, home, eng_dir, ptq_dir, pq2_dir

    def invoke(
        self,
        argv: list[str],
        host_home: Path,
        *,
        xcode_exit: int = 0,
        log_text: str = run_bonsai.PASS_MARKER,
        write_report: bool = True,
    ) -> tuple[int, list[str], dict[str, str]]:
        captured_xcode_cmd: list[str] = []
        captured_xcode_env: dict[str, str] = {}
        real_run = subprocess.run

        def fake_run(
            command: list[str],
            stdout: TextIO | None = None,
            **kwargs: object,
        ) -> subprocess.CompletedProcess[str]:
            if command and command[0] in ("/bin/cp", "cp"):
                return real_run(command, **kwargs)  # type: ignore[arg-type]
            if command and Path(command[0]).name == "llama-server":
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
                    ui_home = Path(env["TEST_RUNNER_INFERYARD_BONSAI_UI_HOME"])
                    settings = json.loads(
                        (
                            ui_home / "Library/Application Support/InferYard Studio/settings.json"
                        ).read_text(encoding="utf-8")
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

        real_prepare = run_bonsai.prepare_environment

        def wrapped_prepare(*args: object, **kwargs: object) -> tuple[Path, Path, Path, bonsai_stage.StagedBonsai]:
            kwargs.setdefault("host_home", host_home)
            return real_prepare(*args, **kwargs)  # type: ignore[arg-type]

        with (
            patch.object(sys, "argv", argv),
            patch.object(run_bonsai.sys, "platform", "darwin"),
            patch.object(run_bonsai, "require_unlocked_desktop"),
            patch.object(run_bonsai, "prepare_environment", side_effect=wrapped_prepare),
            patch.object(run_bonsai.subprocess, "run", side_effect=fake_run),
            patch.object(bonsai_stage.subprocess, "run", side_effect=fake_run),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            code = run_bonsai.main()
        return code, captured_xcode_cmd, captured_xcode_env

    def test_engine_discovery_rejects_old_or_upstream_llama(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            _, _, home, eng_dir, _, _ = self.make_host_assets(root)
            resolved_dir, resolved_bin = bonsai_stage.resolve_local_bonsai_engine(home=home)
            self.assertEqual(resolved_dir, eng_dir)
            self.assertEqual(resolved_bin, eng_dir / "llama-prism-b12232-f702476/llama-server")
            with self.assertRaises(FileNotFoundError):
                bonsai_stage.resolve_local_bonsai_engine(
                    home / ".inferyard/engines/llama-prism-b10743/llama-prism-b10743/llama-server"
                )
            with self.assertRaises(FileNotFoundError):
                bonsai_stage.resolve_local_bonsai_engine(
                    home / ".inferyard/engines/llama-cpp-upstream/llama-server"
                )

    def test_runner_stages_smallest_bonsai_isolates_home_and_preserves_host(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, cli, home, eng_dir, ptq_dir, pq2_dir = self.make_host_assets(root)
            output = root / "output"
            code, xcode_cmd, xcode_env = self.invoke(
                [
                    "run_bonsai.py",
                    "--app",
                    str(app),
                    "--cli",
                    str(cli),
                    "--output-root",
                    str(output),
                    "--keep-workspace",
                ],
                home,
            )
            self.assertEqual(code, 0)
            self.assertIn("-only-testing:PreviewUITests/BonsaiRealUITests", xcode_cmd)
            self.assertNotIn("--debug-ui", xcode_cmd)
            self.assertEqual(
                xcode_env["TEST_RUNNER_INFERYARD_BONSAI_UI_MODEL"],
                "Ternary-Bonsai-2.5-27B-PTQ1_0.gguf",
            )
            self.assertEqual(
                xcode_env["TEST_RUNNER_INFERYARD_BONSAI_UI_ALL_MODELS"],
                "Ternary-Bonsai-2.5-27B-PTQ1_0.gguf,Ternary-Bonsai-2.5-27B-PQ2_0.gguf",
            )
            self.assertEqual(
                xcode_env["TEST_RUNNER_INFERYARD_BONSAI_UI_ENGINE_PREFIX"],
                "llama-cpp-prism-b12232-f702476 · ",
            )

            ui_home = Path(xcode_env["TEST_RUNNER_INFERYARD_BONSAI_UI_HOME"])
            self.assertTrue(ui_home.is_relative_to(output.resolve()))
            settings = json.loads(
                (ui_home / "Library/Application Support/InferYard Studio/settings.json").read_text(
                    encoding="utf-8"
                )
            )
            workspace = Path(settings["workspace_root"])
            self.assertTrue((workspace / "models/ternary-bonsai-2.5-27b-ptq1/Ternary-Bonsai-2.5-27B-PTQ1_0.gguf").is_file())
            self.assertTrue((workspace / "models/ternary-bonsai-2.5-27b-pq2/Ternary-Bonsai-2.5-27B-PQ2_0.gguf").is_file())
            self.assertTrue((workspace / "engines/llama-cpp-prism-b12232-f702476/llama-prism-b12232-f702476/llama-server").is_file())
            self.assertTrue(eng_dir.is_dir() and ptq_dir.is_dir() and pq2_dir.is_dir())

            runs = list(output.iterdir())
            self.assertEqual(len(runs), 1)
            manifest = json.loads((runs[0] / "run.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["suite"], "bonsai-real")
            self.assertEqual(manifest["model"], "Ternary-Bonsai-2.5-27B-PTQ1_0.gguf")
            self.assertTrue((runs[0] / "studio.json").is_file())

    def test_missing_pass_marker_or_studio_report_fails(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, cli, home, _, _, _ = self.make_host_assets(root)
            no_marker_code, _, _ = self.invoke(
                [
                    "run_bonsai.py",
                    "--app",
                    str(app),
                    "--cli",
                    str(cli),
                    "--output-root",
                    str(root / "out1"),
                ],
                home,
                log_text="",
            )
            self.assertEqual(no_marker_code, 65)

            no_report_code, _, _ = self.invoke(
                [
                    "run_bonsai.py",
                    "--app",
                    str(app),
                    "--cli",
                    str(cli),
                    "--output-root",
                    str(root / "out2"),
                ],
                home,
                write_report=False,
            )
            self.assertEqual(no_report_code, 66)


if __name__ == "__main__":
    unittest.main()
