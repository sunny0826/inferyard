"""Run macOS UI smoke against a packaged production app and a real E2E report."""

from __future__ import annotations

import argparse
import contextlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

from run import require_unlocked_desktop


def prepare_home(output: Path, e2e_root: Path, cli: Path) -> tuple[Path, Path]:
    home = output / "home"
    settings_dir = home / "Library/Application Support/InferYard Studio"
    settings_dir.mkdir(parents=True)
    settings_dir.chmod(0o700)
    workspace = e2e_root / "workspace"
    settings = {
        "inferyard_path": str(cli),
        "workspace_root": str(workspace),
        "results_root": str(workspace / "results"),
        "models_root": str(e2e_root / "models"),
        "model_download_name": "model",
        "engine_download_name": "engine",
    }
    settings_path = settings_dir / "settings.json"
    with settings_path.open("w", encoding="utf-8") as stream, contextlib.redirect_stdout(stream):
        print(json.dumps(settings, indent=2))
    settings_path.chmod(0o600)
    return home, workspace


def xcode_command(output: Path) -> list[str]:
    return [
        "xcodebuild", "test",
        "-project", str(Path(__file__).parent / "PreviewUI.xcodeproj"),
        "-scheme", "PreviewUI",
        "-destination", "platform=macOS",
        "-derivedDataPath", str(output / "DerivedData"),
        "-resultBundlePath", str(output / "Results.xcresult"),
        "-parallel-testing-enabled", "NO",
        "-only-testing:PreviewUITests/ReleaseUITests",
        "-test-timeouts-enabled", "YES",
        "-default-test-execution-time-allowance", "300",
        "-maximum-test-execution-time-allowance", "300",
    ]


def execute(command: list[str], env: dict[str, str], output: Path, report: Path) -> int:
    log = output / "xcodebuild.log"
    print(f"Production UI smoke artifacts: {output}", flush=True)
    with log.open("w") as stream:
        started = time.perf_counter()
        result = subprocess.run(
            command, stdout=stream, stderr=subprocess.STDOUT, check=False, env=env
        )
        elapsed = time.perf_counter() - started
    log_text = log.read_text(errors="replace")
    exit_code = result.returncode
    pass_marker = "Test Case '-[PreviewUITests.ReleaseUITests testProductionPagesAndRealReport]' passed"
    if exit_code == 0 and pass_marker not in log_text:
        exit_code = 65
        print("ReleaseUITests did not report a passing result", flush=True)
    manifest = {
        "suite": "release",
        "elapsed_seconds": elapsed,
        "exit_code": exit_code,
        "app": env["TEST_RUNNER_INFERYARD_RELEASE_APP"],
        "report": str(report),
    }
    with (output / "run.json").open("w", encoding="utf-8") as stream, contextlib.redirect_stdout(stream):
        print(json.dumps(manifest, indent=2))
    status = "passed" if exit_code == 0 else "failed"
    print(f"Production UI smoke {status}; build and test: {elapsed:.1f}s", flush=True)
    if exit_code:
        lines = log_text.splitlines()
        failures = [line for line in lines[:-80] if ": error:" in line]
        print("\n".join([*failures, *lines[-80:]]))
    return exit_code


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, required=True)
    parser.add_argument("--e2e-root", type=Path, required=True)
    parser.add_argument("--cli", type=Path, required=True)
    parser.add_argument("--output-root", type=Path, default=Path("build/release-ui-e2e"))
    args = parser.parse_args()
    if sys.platform != "darwin":
        parser.error("production UI smoke requires macOS, a logged-in desktop, and Xcode")
    app, e2e_root, cli = args.app.resolve(), args.e2e_root.resolve(), args.cli.resolve()
    if not app.is_dir():
        parser.error(f"packaged .app is missing: {app}")
    if not cli.is_file():
        parser.error(f"CLI executable is missing: {cli}")
    reports = sorted(e2e_root.glob("workspace/reports/*/studio.json"))
    if len(reports) != 1:
        parser.error(f"expected one real E2E studio.json, found {len(reports)}")
    try:
        require_unlocked_desktop()
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        parser.error(f"desktop preflight failed: {exc}")

    args.output_root.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="run-", dir=args.output_root.resolve()))
    home, workspace = prepare_home(output, e2e_root, cli)
    env = dict(
        os.environ,
        TEST_RUNNER_INFERYARD_RELEASE_APP=str(app),
        TEST_RUNNER_INFERYARD_RELEASE_HOME=str(home),
        TEST_RUNNER_INFERYARD_RELEASE_CLI=str(cli),
        TEST_RUNNER_INFERYARD_RELEASE_WORKSPACE=str(workspace),
        TEST_RUNNER_INFERYARD_RELEASE_RESULTS=str(workspace / "results"),
        TEST_RUNNER_INFERYARD_RELEASE_MODELS=str(e2e_root / "models"),
    )
    return execute(xcode_command(output), env, output, reports[0])


if __name__ == "__main__":
    raise SystemExit(main())
