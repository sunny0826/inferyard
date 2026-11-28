"""Run local-only real macOS window UI E2E against existing local Bonsai 2.5 assets (not in CI)."""

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

from bonsai_stage import StagedBonsai, stage_local_bonsai, verify_host_bonsai_unchanged
from run import require_unlocked_desktop, resolve_derived_data
from run_polystrata import build_local_studio_app, default_cli_path, terminate_workspace_processes

PASS_MARKER = (
    "Test Case '-[PreviewUITests.BonsaiRealUITests testRealBonsaiStudioLifecycle]' passed"
)

__all__ = [
    "PASS_MARKER",
    "StagedBonsai",
    "main",
    "prepare_environment",
    "stage_local_bonsai",
    "verify_host_bonsai_unchanged",
]


def prepare_environment(
    output: Path,
    cli: Path,
    *,
    engine_override: Path | None = None,
    model_override: Path | None = None,
    host_home: Path | None = None,
) -> tuple[Path, Path, Path, StagedBonsai]:
    user_home = host_home if host_home is not None else Path.home()
    host_dir = output / "host"
    host_dir.mkdir(parents=True, exist_ok=True)
    bootstrap = Path(__file__).resolve().parents[1] / "e2e/cli_bootstrap.py"
    shutil.copyfile(bootstrap, host_dir / "sitecustomize.py")
    existing_py = os.environ.get("PYTHONPATH", "").strip()
    python_path = f"{host_dir}{os.pathsep}{existing_py}" if existing_py else str(host_dir)

    workspace = output / "workspace"
    cli_env = dict(os.environ, PYTHONPATH=python_path)
    subprocess.run([str(cli), "init", "--out", str(workspace)], env=cli_env, check=True)
    staged = stage_local_bonsai(
        workspace,
        engine_override=engine_override,
        model_override=model_override,
        home=user_home,
    )

    home = output / "home"
    settings_dir = home / "Library/Application Support/InferYard Studio"
    settings_dir.mkdir(parents=True, exist_ok=True)
    settings_dir.chmod(0o700)
    settings = {
        "inferyard_path": str(cli),
        "workspace_root": str(workspace),
        "keep_services_on_quit": False,
    }
    settings_path = settings_dir / "settings.json"
    settings_path.write_text(json.dumps(settings, indent=2) + "\n", encoding="utf-8")
    settings_path.chmod(0o600)
    return home, workspace, Path(python_path), staged


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
        "-only-testing:PreviewUITests/BonsaiRealUITests",
        "-test-timeouts-enabled",
        "YES",
        "-default-test-execution-time-allowance",
        "1200",
        "-maximum-test-execution-time-allowance",
        "1200",
    ]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, default=None, help="prebuilt InferYard Studio.app")
    parser.add_argument("--cli", type=Path, default=None, help="inferyard CLI executable")
    parser.add_argument("--engine", type=Path, default=None, help="PrismML llama-cpp dir or binary")
    parser.add_argument("--model", type=Path, default=None, help="Bonsai 2.5 GGUF file or dir")
    parser.add_argument("--output-root", type=Path, default=Path("build/bonsai-ui-e2e"))
    parser.add_argument("--derived-data", type=Path, default=None)
    parser.add_argument("--keep-workspace", action="store_true")
    args = parser.parse_args()

    if sys.platform != "darwin":
        parser.error("local Bonsai native UI E2E requires macOS, an unlocked desktop, and Xcode")
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
        plist_doc["CFBundleIdentifier"] = f"local.inferyard.studio-bonsai-ui.{output.name}"
        plist_path.write_bytes(plistlib.dumps(plist_doc))

    try:
        home, workspace, python_path, staged = prepare_environment(
            output,
            cli,
            engine_override=args.engine,
            model_override=args.model,
        )
    except FileNotFoundError as exc:
        shutil.rmtree(output, ignore_errors=True)
        parser.error(str(exc))

    log = output / "xcodebuild.log"
    exit_code = 1
    try:
        print(f"Bonsai real UI E2E artifacts: {output}", flush=True)
        print(f"Staged primary Bonsai model: {staged.chosen_model_filename}", flush=True)
        print(f"Staged PrismML engine prefix: {staged.engine_prefix}", flush=True)
        env = dict(
            os.environ,
            TEST_RUNNER_INFERYARD_BONSAI_UI_APP=str(app),
            TEST_RUNNER_INFERYARD_BONSAI_UI_HOME=str(home),
            TEST_RUNNER_INFERYARD_BONSAI_UI_PYTHONPATH=str(python_path),
            TEST_RUNNER_INFERYARD_BONSAI_UI_MODEL=staged.chosen_model_filename,
            TEST_RUNNER_INFERYARD_BONSAI_UI_ALL_MODELS=",".join(staged.all_model_filenames),
            TEST_RUNNER_INFERYARD_BONSAI_UI_ENGINE_PREFIX=staged.engine_prefix,
        )
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
            print("BonsaiRealUITests did not report a passing result", flush=True)

        reports = sorted(workspace.glob("reports/*/studio.json"))
        copied_report = ""
        if exit_code == 0:
            if not reports:
                exit_code = 66
                print("BonsaiRealUITests produced no studio.json report", flush=True)
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
            "suite": "bonsai-real",
            "model": staged.chosen_model_filename,
            "all_models": list(staged.all_model_filenames),
            "engine_prefix": staged.engine_prefix,
            "elapsed_seconds": elapsed,
            "exit_code": exit_code,
            "app": str(app),
            "report": copied_report,
        }
        (output / "run.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        status = "passed" if exit_code == 0 else "failed"
        print(f"Bonsai real UI E2E {status}; build and test: {elapsed:.1f}s", flush=True)
        if exit_code:
            lines = log_text.splitlines()
            failures = [line for line in lines[:-200] if ": error:" in line]
            print("\n".join([*failures, *lines[-200:]]))
    finally:
        terminate_workspace_processes(workspace)
        if not args.keep_workspace:
            shutil.rmtree(workspace, ignore_errors=True)
            shutil.rmtree(home / ".cache", ignore_errors=True)
        verify_host_bonsai_unchanged(staged)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
