"""Unit checks for multi-engine real UI staging and post-upgrade verification (no window)."""

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

import engines_stage
import run_engines


class TestEnginesRealRunner(unittest.TestCase):
    def make_cached_fixtures(self, root: Path) -> tuple[Path, Path, Path, Path]:
        app = root / "package/InferYard Studio.app"
        app.mkdir(parents=True)
        cli = root / "cli/inferyard"
        cli.parent.mkdir(parents=True)
        cli.write_text("#!/bin/sh\n", encoding="utf-8")

        cache = root / "cache"
        llama_bin = cache / "engines/llama-prism-b10743/llama-server"
        llama_bin.parent.mkdir(parents=True)
        llama_bin.write_text("#!/bin/sh\n", encoding="utf-8")
        llama_bin.chmod(0o755)
        gguf = cache / "models/smollm2-gguf/SmolLM2-135M-Instruct-Q4_K_M.gguf"
        gguf.parent.mkdir(parents=True)
        gguf.write_bytes(b"GGUF" * 32)

        for name, dist_name, ver in (
            ("mlx-lm-base", "mlx_lm", "0.31.1"),
            ("vllm-base", "vllm", "0.31.0+cpu"),
        ):
            py = cache / f"engines/{name}/bin/python3"
            py.parent.mkdir(parents=True)
            py.write_text("#!/bin/sh\n", encoding="utf-8")
            py.chmod(0o755)
            meta = (
                cache
                / f"engines/{name}/lib/python3.12/site-packages/{dist_name}-{ver}.dist-info/METADATA"
            )
            meta.parent.mkdir(parents=True)
            meta.write_text(f"Metadata-Version: 2.1\nName: {dist_name}\nVersion: {ver}\n")
        metal_meta = (
            cache
            / "engines/vllm-base/lib/python3.12/site-packages/vllm_metal-0.31.0.dist-info/METADATA"
        )
        metal_meta.parent.mkdir(parents=True)
        metal_meta.write_text("Metadata-Version: 2.1\nName: vllm-metal\nVersion: 0.31.0\n")

        mlx_cfg = cache / "models/smollm2-mlx/config.json"
        mlx_cfg.parent.mkdir(parents=True)
        mlx_cfg.write_text("{}", encoding="utf-8")
        hf_weights = cache / "models/smollm2-hf/model.safetensors"
        hf_weights.parent.mkdir(parents=True)
        hf_weights.write_bytes(b"SAFE" * 32)
        (hf_weights.parent / "config.json").write_text("{}", encoding="utf-8")

        host_home = root / "host_home"
        ps_work = host_home / ".inferyard/engines/polystrata-v0.2.20-poly.5"
        ps_data = host_home / ".inferyard/models/polystrata-data"
        (ps_work / "terminal").mkdir(parents=True)
        (ps_work / "engine/build-metal").mkdir(parents=True)
        (ps_data / "packs/q2_0").mkdir(parents=True)
        strata_bin = ps_work / "engine/build-metal/strata"
        strata_bin.write_text("#!/bin/sh\n", encoding="utf-8")
        strata_bin.chmod(0o755)
        (ps_work / "engine/build-metal/BUILD-poly.json").write_text(
            json.dumps({"version": "0.2.20", "backend": "metal"}), encoding="utf-8"
        )
        (ps_work / ".inferyard-runtime.json").write_text(
            json.dumps({"definition": "polystrata-runtime.v1", "source": "https://example.com/a.tgz"}),
            encoding="utf-8",
        )
        (ps_data / "packs/q2_0/dense.bin").write_bytes(b"p" * 512)
        (ps_work / "terminal/strata-q2_0.json").write_text(
            json.dumps(
                {
                    "exe": str(strata_bin),
                    "args": ["--pack", str(ps_data / "packs/q2_0")],
                    "model_name": "qwen3.8-flash-next-q2_0",
                }
            ),
            encoding="utf-8",
        )
        ps_inst = ps_work / "installation.json"
        ps_inst.write_text(
            json.dumps(
                {
                    "family": "polystrata",
                    "working_directory": str(ps_work),
                    "data_dir": str(ps_data),
                }
            ),
            encoding="utf-8",
        )
        return app, cli, cache, host_home

    def invoke(
        self,
        argv: list[str],
        *,
        host_home: Path,
        simulate_upgrades: bool = True,
        write_report: bool = True,
    ) -> tuple[int, list[str], dict[str, str]]:
        captured_cmd: list[str] = []
        captured_env: dict[str, str] = {}
        real_run = subprocess.run

        def fake_run(
            command: list[str],
            stdout: TextIO | None = None,
            **kwargs: object,
        ) -> subprocess.CompletedProcess[str]:
            if command and command[0] in ("/bin/cp", "cp"):
                return real_run(command, **kwargs)  # type: ignore[arg-type]
            if len(command) >= 4 and command[1] == "init" and command[2] == "--out":
                ws = Path(command[3])
                for sub in ("models", "engines", "results", "reports"):
                    (ws / sub).mkdir(parents=True, exist_ok=True)
                return subprocess.CompletedProcess(command, 0, "", "")
            if len(command) >= 5 and command[1] == "engine" and command[2] == "init-installation":
                out_idx = command.index("--out")
                fam_idx = command.index("--family")
                inst_out = Path(command[out_idx + 1])
                inst_out.parent.mkdir(parents=True, exist_ok=True)
                inst_out.write_text(
                    json.dumps({"family": command[fam_idx + 1], "schema_version": 3}),
                    encoding="utf-8",
                )
                return subprocess.CompletedProcess(command, 0, "", "")
            if command and command[0] == "xcodebuild":
                captured_cmd.extend(command)
                env = kwargs.get("env")
                if isinstance(env, dict):
                    captured_env.update(env)
                if stdout is not None:
                    stdout.write(run_engines.PASS_MARKER)
                if isinstance(env, dict):
                    home = Path(env["TEST_RUNNER_INFERYARD_ENGINES_UI_HOME"])
                    settings = json.loads(
                        (home / "Library/Application Support/InferYard Studio/settings.json").read_text(
                            encoding="utf-8"
                        )
                    )
                    ws = Path(settings["workspace_root"])
                    selected = env["TEST_RUNNER_INFERYARD_ENGINES_UI_ENGINES"].split(",")
                    if simulate_upgrades:
                        self._simulate_disk_upgrades(ws, selected)
                    if write_report:
                        rep = ws / "reports/summary-001"
                        rep.mkdir(parents=True, exist_ok=True)
                        (rep / "studio.json").write_text(
                            json.dumps({"schema_version": 3}), encoding="utf-8"
                        )
                return subprocess.CompletedProcess(command, 0, "", "")
            if command and command[0] == "ps":
                return subprocess.CompletedProcess(command, 0, "", "")
            raise AssertionError(f"unexpected subprocess command: {command}")

        orig_stage = run_engines.stage_engines_environment

        def stage_with_home(*args: object, **kwargs: object) -> engines_stage.StagedEnginesEnv:
            kwargs["host_home"] = host_home
            return orig_stage(*args, **kwargs)  # type: ignore[arg-type]

        with (
            patch.object(sys, "argv", argv),
            patch.object(run_engines.sys, "platform", "darwin"),
            patch.object(run_engines, "require_unlocked_desktop"),
            patch.object(run_engines, "stage_engines_environment", side_effect=stage_with_home),
            patch.object(run_engines.subprocess, "run", side_effect=fake_run),
            patch.object(engines_stage.subprocess, "run", side_effect=fake_run),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            code = run_engines.main()
        return code, captured_cmd, captured_env

    def _simulate_disk_upgrades(self, ws: Path, selected: list[str]) -> None:
        if "llama-cpp" in selected:
            old_dir = ws / "engines/llama-prism-b10743"
            if old_dir.exists():
                for p in sorted(old_dir.rglob("*"), reverse=True):
                    if p.is_file():
                        p.unlink()
                    elif p.is_dir():
                        p.rmdir()
                old_dir.rmdir()
            up_dir = ws / "engines/llama-cpp-prism-b12232-f702476"
            up_dir.mkdir(parents=True, exist_ok=True)
            (up_dir / "llama-server").write_text("#!/bin/sh\n", encoding="utf-8")
            (up_dir / ".inferyard-managed-engine.json").write_text(
                json.dumps(
                    {
                        "definition": "inferyard-managed-engine.v1",
                        "family": "llama-cpp",
                        "install_mode": "binary",
                        "version": "prism-b12232-f702476",
                    }
                ),
                encoding="utf-8",
            )
        if "mlx-lm" in selected:
            site = ws / "engines/mlx-lm-managed/venv/lib/python3.12/site-packages"
            for old in site.glob("mlx_lm-*.dist-info"):
                for child in old.iterdir():
                    child.unlink()
                old.rmdir()
            up_meta = site / "mlx_lm-0.32.2.dist-info/METADATA"
            up_meta.parent.mkdir(parents=True, exist_ok=True)
            up_meta.write_text("Name: mlx-lm\nVersion: 0.32.2\n", encoding="utf-8")
        if "vllm" in selected:
            site = ws / "engines/vllm-managed/venv/lib/python3.12/site-packages"
            for prefix in ("vllm-", "vllm_metal-"):
                for old in site.glob(f"{prefix}*.dist-info"):
                    for child in old.iterdir():
                        child.unlink()
                    old.rmdir()
            for dist, ver in (("vllm", "0.34.0+cpu"), ("vllm_metal", "0.34.0")):
                meta = site / f"{dist}-{ver}.dist-info/METADATA"
                meta.parent.mkdir(parents=True, exist_ok=True)
                meta.write_text(f"Name: {dist}\nVersion: {ver}\n", encoding="utf-8")
        if "polystrata" in selected:
            ps_up = ws / "engines/polystrata-v0.2.20-poly.6"
            ps_up.mkdir(parents=True, exist_ok=True)
            (ps_up / "installation.json").write_text('{"family":"polystrata"}', encoding="utf-8")
            (ps_up / ".inferyard-runtime.json").write_text(
                '{"definition":"polystrata-runtime.v1"}', encoding="utf-8"
            )

    def test_parse_engines_arg_supports_all_lightweight_and_subsets(self) -> None:
        self.assertEqual(
            engines_stage.parse_engines_arg("all"),
            ("llama-cpp", "mlx-lm", "vllm", "polystrata"),
        )
        self.assertEqual(
            engines_stage.parse_engines_arg("all", lightweight=True),
            ("llama-cpp", "mlx-lm", "vllm"),
        )
        self.assertEqual(
            engines_stage.parse_engines_arg("vllm,llama-cpp,vllm"),
            ("vllm", "llama-cpp"),
        )
        with self.assertRaises(ValueError):
            engines_stage.parse_engines_arg("lmstudio")

    def test_full_runner_stages_four_engines_verifies_upgrades_and_preserves_host(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, cli, cache, host_home = self.make_cached_fixtures(root)
            ps_inst = host_home / ".inferyard/engines/polystrata-v0.2.20-poly.5/installation.json"
            output = root / "output"
            code, xcode_cmd, xcode_env = self.invoke(
                [
                    "run_engines.py",
                    "--app",
                    str(app),
                    "--cli",
                    str(cli),
                    "--cache-dir",
                    str(cache),
                    "--installation",
                    str(ps_inst),
                    "--output-root",
                    str(output),
                    "--keep-workspace",
                ],
                host_home=host_home,
            )
            self.assertEqual(code, 0)
            self.assertIn("-only-testing:PreviewUITests/EnginesRealUITests", xcode_cmd)
            self.assertEqual(
                xcode_env["TEST_RUNNER_INFERYARD_ENGINES_UI_ENGINES"],
                "llama-cpp,mlx-lm,vllm,polystrata",
            )
            self.assertEqual(
                xcode_env["TEST_RUNNER_INFERYARD_ENGINES_UI_POLYSTRATA_MODEL"],
                "qwen3.8-flash-next-q2_0",
            )

            runs = list(output.iterdir())
            self.assertEqual(len(runs), 1)
            manifest = json.loads((runs[0] / "run.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["suite"], "engines-real")
            self.assertEqual(
                set(manifest["upgraded_versions"].keys()),
                {"llama-cpp", "mlx-lm", "vllm", "polystrata"},
            )
            # Staged PolyStrata had its receipt removed and version downgraded, while host kept its receipt.
            self.assertTrue((ps_inst.parent / ".inferyard-runtime.json").is_file())
            staged_build = json.loads(
                (
                    runs[0] / "workspace/engines/polystrata/engine/build-metal/BUILD-poly.json"
                ).read_text(encoding="utf-8")
            )
            self.assertEqual(staged_build["version"], "v0.2.20-poly.4")

    def test_lightweight_mode_excludes_polystrata_and_detects_missing_disk_upgrade(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            app, cli, cache, host_home = self.make_cached_fixtures(root)
            # Delete host PolyStrata completely to prove --lightweight does not require it.
            for p in sorted((host_home / ".inferyard").rglob("*"), reverse=True):
                if p.is_file():
                    p.unlink()
                elif p.is_dir():
                    p.rmdir()

            ok_code, _, env_ok = self.invoke(
                [
                    "run_engines.py",
                    "--lightweight",
                    "--app",
                    str(app),
                    "--cli",
                    str(cli),
                    "--cache-dir",
                    str(cache),
                    "--output-root",
                    str(root / "out_ok"),
                ],
                host_home=host_home,
            )
            self.assertEqual(ok_code, 0)
            self.assertEqual(
                env_ok["TEST_RUNNER_INFERYARD_ENGINES_UI_ENGINES"], "llama-cpp,mlx-lm,vllm"
            )

            bad_code, _, _ = self.invoke(
                [
                    "run_engines.py",
                    "--lightweight",
                    "--app",
                    str(app),
                    "--cli",
                    str(cli),
                    "--cache-dir",
                    str(cache),
                    "--output-root",
                    str(root / "out_bad"),
                ],
                host_home=host_home,
                simulate_upgrades=False,
            )
            self.assertEqual(bad_code, 67)

    def test_cold_cache_acquisition_invokes_cli_prepare_and_model_acquire(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            cli = root / "cli/inferyard"
            cli.parent.mkdir(parents=True)
            cli.write_text("#!/bin/sh\n", encoding="utf-8")
            cache = root / "empty_cache"
            host_home = root / "host_home"
            host_home.mkdir()
            recorded: list[list[str]] = []
            real_run = subprocess.run

            def fake_cold_run(
                command: list[str], **kwargs: object
            ) -> subprocess.CompletedProcess[str]:
                if command and command[0] in ("/bin/cp", "cp"):
                    return real_run(command, **kwargs)  # type: ignore[arg-type]
                recorded.append(list(command))
                if len(command) >= 4 and command[1] == "init":
                    for sub in ("models", "engines", "results", "reports"):
                        (Path(command[3]) / sub).mkdir(parents=True, exist_ok=True)
                elif command[1:3] == ["engine", "prepare"]:
                    out = Path(command[command.index("--out") + 1])
                    out.mkdir(parents=True, exist_ok=True)
                    (out / "llama-server").write_text("#!/bin/sh\n", encoding="utf-8")
                elif command[1:3] == ["model", "acquire"]:
                    out = Path(command[command.index("--out") + 1])
                    out.mkdir(parents=True, exist_ok=True)
                    if command[command.index("--kind") + 1] == "gguf":
                        (out / engines_stage.GGUF_MODEL_FILE).write_bytes(b"GGUF")
                    else:
                        (out / "config.json").write_text("{}", encoding="utf-8")
                elif command[:2] == ["/fake/uv", "venv"]:
                    py = Path(command[-1]) / "bin/python3"
                    py.parent.mkdir(parents=True, exist_ok=True)
                    py.write_text("#!/bin/sh\n", encoding="utf-8")
                elif command[1:3] == ["engine", "init-installation"]:
                    out = Path(command[command.index("--out") + 1])
                    out.parent.mkdir(parents=True, exist_ok=True)
                    out.write_text('{"schema_version":3}', encoding="utf-8")
                return subprocess.CompletedProcess(command, 0, "", "")

            def fake_dl(url: str, dst: Path) -> None:
                dst.parent.mkdir(parents=True, exist_ok=True)
                dst.write_bytes(b"HF")

            with (
                patch.object(engines_stage, "_find_uv", return_value="/fake/uv"),
                patch.object(engines_stage, "_download_file", side_effect=fake_dl),
                patch.object(engines_stage.subprocess, "run", side_effect=fake_cold_run),
            ):
                staged = engines_stage.stage_engines_environment(
                    root / "out",
                    cli,
                    cache,
                    engines_stage.LIGHTWEIGHT_ENGINES,
                    host_home=host_home,
                )

            self.assertTrue((staged.workspace / "models/smollm2-gguf/SmolLM2-135M-Instruct-Q4_K_M.gguf").is_file())
            self.assertTrue((staged.workspace / "models/smollm2-mlx/config.json").is_file())
            self.assertTrue((staged.workspace / "models/smollm2-hf/model.safetensors").is_file())
            prepare_cmds = [c for c in recorded if c[1:3] == ["engine", "prepare"]]
            acquire_cmds = [c for c in recorded if c[1:3] == ["model", "acquire"]]
            self.assertEqual(len(prepare_cmds), 1)
            self.assertIn("--expected-sha256", prepare_cmds[0])
            self.assertEqual(len(acquire_cmds), 2)


_TEST_METHODS = (
    TestEnginesRealRunner.test_parse_engines_arg_supports_all_lightweight_and_subsets,
    TestEnginesRealRunner.test_full_runner_stages_four_engines_verifies_upgrades_and_preserves_host,
    TestEnginesRealRunner.test_lightweight_mode_excludes_polystrata_and_detects_missing_disk_upgrade,
    TestEnginesRealRunner.test_cold_cache_acquisition_invokes_cli_prepare_and_model_acquire,
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
