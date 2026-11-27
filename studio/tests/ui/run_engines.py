"""Run real macOS window UI E2E verifying one-click upgrade and execution across engines."""

from __future__ import annotations

import argparse
import json
import os
import plistlib
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

from engines_stage import (
    ALL_UPGRADABLE_ENGINES,
    LIGHTWEIGHT_ENGINES,
    MLX_BASE_VERSION,
    POLYSTRATA_DOWNGRADED_VERSION,
    VLLM_BASE_VERSION,
    StagedEnginesEnv,
    parse_engines_arg,
    stage_engines_environment,
    verify_host_assets_unchanged,
)
from run import require_unlocked_desktop, resolve_derived_data
from run_polystrata import build_local_studio_app, default_cli_path, terminate_workspace_processes

PASS_MARKER = (
    "Test Case '-[PreviewUITests.EnginesRealUITests testAllSupportedEnginesUpgradeAndRun]' passed"
)

__all__ = [
    "ALL_UPGRADABLE_ENGINES",
    "LIGHTWEIGHT_ENGINES",
    "PASS_MARKER",
    "main",
    "parse_engines_arg",
    "stage_engines_environment",
    "verify_post_upgrade_disk_state",
]


def xcode_command(output: Path, derived_data: Path) -> list[str]:
    return [
        "xcodebuild",
        "test",
        "-project",
        str(Path(__file__).parent / "PreviewUI.xcodeproj"),
        "-scheme",
        "PreviewUI",
        "-destination",
        "platform=macOS",
        "-derivedDataPath",
        str(derived_data),
        "-resultBundlePath",
        str(output / "Results.xcresult"),
        "-parallel-testing-enabled",
        "NO",
        "-only-testing:PreviewUITests/EnginesRealUITests",
        "-test-timeouts-enabled",
        "YES",
        "-default-test-execution-time-allowance",
        "2400",
        "-maximum-test-execution-time-allowance",
        "2400",
    ]


def _read_dist_info_version(venv_dir: Path, dist_prefix: str) -> str:
    for meta in sorted(venv_dir.glob(f"lib/python*/site-packages/{dist_prefix}-*.dist-info/METADATA")):
        for line in meta.read_text(encoding="utf-8", errors="replace").splitlines():
            if line.startswith("Version:"):
                return line.partition(":")[2].strip()
    return ""


def _semver_tuple(raw: str) -> tuple[int, ...]:
    base = raw.strip().lstrip("vV").split("+", 1)[0].split("-", 1)[0]
    parts = [int(seg) for seg in base.split(".") if seg.isdigit()]
    return tuple(parts)


def verify_post_upgrade_disk_state(
    workspace: Path, engines: tuple[str, ...]
) -> tuple[dict[str, str], list[str]]:
    versions: dict[str, str] = {}
    errors: list[str] = []
    engines_root = workspace / "engines"

    if "llama-cpp" in engines:
        upgraded_llama = ""
        for child in sorted(engines_root.glob("llama-cpp*")):
            receipt_file = child / ".inferyard-managed-engine.json"
            if receipt_file.is_file() and any(child.rglob("llama-server")):
                doc = json.loads(receipt_file.read_text(encoding="utf-8"))
                ver = str(doc.get("version") or "").strip()
                if doc.get("install_mode") == "binary" and ver and "b10743" not in ver:
                    upgraded_llama = ver
                    break
        if upgraded_llama:
            versions["llama-cpp"] = upgraded_llama
            if (engines_root / "llama-prism-b10743").exists():
                errors.append("replaced llama-prism-b10743 directory was not cleaned up")
        else:
            errors.append("llama-cpp one-click upgrade did not create a verified managed binary")

    if "mlx-lm" in engines:
        mlx_ver = _read_dist_info_version(engines_root / "mlx-lm-managed/venv", "mlx_lm")
        if mlx_ver and _semver_tuple(mlx_ver) > _semver_tuple(MLX_BASE_VERSION):
            versions["mlx-lm"] = mlx_ver
        else:
            errors.append(
                f"mlx-lm venv was not upgraded past {MLX_BASE_VERSION} (found {mlx_ver!r})"
            )

    if "vllm" in engines:
        vllm_venv = engines_root / "vllm-managed/venv"
        vllm_ver = _read_dist_info_version(vllm_venv, "vllm")
        metal_ver = _read_dist_info_version(vllm_venv, "vllm_metal")
        if (
            vllm_ver
            and metal_ver
            and _semver_tuple(vllm_ver) > _semver_tuple(VLLM_BASE_VERSION)
            and _semver_tuple(metal_ver) > _semver_tuple(VLLM_BASE_VERSION)
        ):
            versions["vllm"] = f"{vllm_ver} (vllm-metal {metal_ver})"
        else:
            errors.append(
                f"vllm venv was not upgraded past {VLLM_BASE_VERSION} "
                f"(vllm={vllm_ver!r}, vllm-metal={metal_ver!r})"
            )

    if "polystrata" in engines:
        upgraded_ps = ""
        for child in sorted(engines_root.glob("polystrata-*")):
            inst_file = child / "installation.json"
            receipt_file = child / ".inferyard-runtime.json"
            if inst_file.is_file() and receipt_file.is_file():
                upgraded_ps = child.name.removeprefix("polystrata-")
                if upgraded_ps and upgraded_ps != POLYSTRATA_DOWNGRADED_VERSION:
                    break
        if upgraded_ps:
            versions["polystrata"] = upgraded_ps
        else:
            errors.append("polystrata one-click upgrade did not produce a new polystrata-* directory")

    return versions, errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, default=None, help="prebuilt InferYard Studio.app")
    parser.add_argument("--cli", type=Path, default=None, help="inferyard CLI executable")
    parser.add_argument(
        "--engines",
        type=str,
        default="all",
        help="comma-separated engines (llama-cpp,mlx-lm,vllm,polystrata), 'lightweight', or 'all'",
    )
    parser.add_argument(
        "--lightweight",
        action="store_true",
        help="run CI-ready 3-engine lightweight tier (llama-cpp,mlx-lm,vllm with SmolLM2-135M)",
    )
    parser.add_argument("--installation", type=Path, default=None, help="local PolyStrata installation.json")
    parser.add_argument("--cache-dir", type=Path, default=Path("build/engines-ui-cache"))
    parser.add_argument("--output-root", type=Path, default=Path("build/engines-ui-e2e"))
    parser.add_argument("--derived-data", type=Path, default=None)
    parser.add_argument("--keep-workspace", action="store_true")
    args = parser.parse_args()

    if sys.platform != "darwin":
        parser.error("real-machine Studio engines UI E2E requires macOS, an unlocked desktop, and Xcode")
    try:
        engines = parse_engines_arg(args.engines, lightweight=args.lightweight)
    except ValueError as exc:
        parser.error(str(exc))

    cli = (args.cli or default_cli_path()).expanduser().resolve()
    if not cli.is_file():
        parser.error(f"CLI executable is missing: {cli} (run `mise run cli:sync` first)")
    try:
        require_unlocked_desktop()
    except (
        OSError,
        TypeError,
        ValueError,
        plistlib.InvalidFileException,
        subprocess.SubprocessError,
    ) as exc:
        parser.error(f"desktop preflight failed: {exc}")

    args.output_root.mkdir(parents=True, exist_ok=True)
    cache_dir = args.cache_dir.expanduser().resolve()
    cache_dir.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="run-", dir=args.output_root.resolve()))
    derived_data = resolve_derived_data(args.output_root, args.derived_data)
    derived_data.mkdir(parents=True, exist_ok=True)
    studio_dir = Path(__file__).resolve().parents[2]
    app = args.app.expanduser().resolve() if args.app else build_local_studio_app(studio_dir, output)
    if not app.is_dir():
        parser.error(f"Studio .app bundle is missing: {app}")
    if args.app is None:
        plist_path = app / "Contents/Info.plist"
        plist_doc = plistlib.loads(plist_path.read_bytes())
        plist_doc["CFBundleIdentifier"] = f"local.inferyard.studio-engines-ui.{output.name}"
        plist_path.write_bytes(plistlib.dumps(plist_doc))

    try:
        staged: StagedEnginesEnv = stage_engines_environment(
            output,
            cli,
            cache_dir,
            engines,
            polystrata_installation=args.installation,
        )
    except FileNotFoundError as exc:
        shutil.rmtree(output, ignore_errors=True)
        parser.error(str(exc))

    log = output / "xcodebuild.log"
    exit_code = 1
    try:
        print(f"Engines real UI E2E artifacts: {output}", flush=True)
        print(f"Selected engines: {', '.join(engines)}", flush=True)
        env = dict(
            os.environ,
            TEST_RUNNER_INFERYARD_ENGINES_UI_APP=str(app),
            TEST_RUNNER_INFERYARD_ENGINES_UI_HOME=str(staged.home),
            TEST_RUNNER_INFERYARD_ENGINES_UI_PYTHONPATH=str(staged.python_path),
            TEST_RUNNER_INFERYARD_ENGINES_UI_ENGINES=",".join(engines),
            TEST_RUNNER_INFERYARD_ENGINES_UI_POLYSTRATA_MODEL=(
                staged.polystrata.model_name if staged.polystrata is not None else ""
            ),
        )
        for api_key in ("INFERYARD_ENGINE_UPDATE_GITHUB_API", "INFERYARD_ENGINE_UPDATE_PYPI_API"):
            val = os.environ.get(api_key, "").strip()
            if val:
                env[f"TEST_RUNNER_{api_key}"] = val

        with log.open("w", encoding="utf-8") as stream:
            started = time.perf_counter()
            result = subprocess.run(
                xcode_command(output, derived_data),
                stdout=stream,
                stderr=subprocess.STDOUT,
                check=False,
                env=env,
            )
            elapsed = time.perf_counter() - started
        log_text = log.read_text(encoding="utf-8", errors="replace")
        exit_code = result.returncode
        if exit_code == 0 and PASS_MARKER not in log_text:
            exit_code = 65
            print("EnginesRealUITests did not report a passing result", flush=True)

        upgraded_versions, upgrade_errors = verify_post_upgrade_disk_state(staged.workspace, engines)
        if exit_code == 0 and upgrade_errors:
            exit_code = 67
            for err_msg in upgrade_errors:
                print(f"Post-upgrade verification failed: {err_msg}", flush=True)

        reports = sorted(staged.workspace.glob("reports/*/studio.json"))
        copied_report = ""
        if exit_code == 0:
            if not reports:
                exit_code = 66
                print("EnginesRealUITests produced no studio.json report", flush=True)
            else:
                report_doc = json.loads(reports[-1].read_text(encoding="utf-8"))
                if report_doc.get("schema_version") != 3:
                    exit_code = 66
                    print("Generated studio.json did not match schema_version=3", flush=True)
                else:
                    dst_report = output / "studio.json"
                    shutil.copyfile(reports[-1], dst_report)
                    copied_report = str(dst_report)

        manifest = {
            "suite": "engines-real-lightweight" if engines == LIGHTWEIGHT_ENGINES else "engines-real",
            "engines": list(engines),
            "upgraded_versions": upgraded_versions,
            "upgrade_errors": upgrade_errors,
            "elapsed_seconds": elapsed,
            "exit_code": exit_code,
            "app": str(app),
            "report": copied_report,
        }
        (output / "run.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        status = "passed" if exit_code == 0 else "failed"
        print(f"Engines real UI E2E {status}; build and test: {elapsed:.1f}s", flush=True)
        if upgraded_versions:
            print(f"Upgraded versions: {json.dumps(upgraded_versions, ensure_ascii=False)}", flush=True)
        if exit_code:
            lines = log_text.splitlines()
            failures = [line for line in lines[:-200] if ": error:" in line]
            print("\n".join([*failures, *lines[-200:]]))
    finally:
        terminate_workspace_processes(staged.workspace)
        if not args.keep_workspace:
            shutil.rmtree(staged.workspace, ignore_errors=True)
            shutil.rmtree(staged.home / ".cache", ignore_errors=True)
        verify_host_assets_unchanged(staged)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
