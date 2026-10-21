"""Run macOS preview UI tests; keep each run's logs and xcresult in a new directory."""

from __future__ import annotations

import argparse
import json
import os
import plistlib
import subprocess
import sys
import tempfile
import time
from pathlib import Path


def require_unlocked_desktop() -> None:
    result = subprocess.run(["ioreg", "-n", "Root", "-d1", "-a"], capture_output=True, check=True)
    state = plistlib.loads(result.stdout)
    if isinstance(state, list) and len(state) == 1:
        state = state[0]
    if not isinstance(state, dict) or not isinstance(state.get("IOConsoleUsers"), list):
        raise ValueError("cannot determine the active macOS desktop session")
    sessions = [
        item
        for item in state["IOConsoleUsers"]
        if isinstance(item, dict) and item.get("kCGSSessionOnConsoleKey") is True
    ]
    if len(sessions) != 1 or sessions[0].get("kCGSessionLoginDoneKey") is not True:
        raise ValueError("native UI e2e requires a logged-in console desktop")
    if sessions[0].get("CGSSessionScreenIsLocked", False) is not False:
        raise ValueError("unlock the macOS desktop and keep it unlocked during native UI e2e")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-root", type=Path, default=Path("build/ui-e2e"))
    parser.add_argument(
        "--suite",
        choices=("daily", "full"),
        default="daily",
        help="daily: 3 key workflows (default); full: all 12 workflows",
    )
    args = parser.parse_args()
    if sys.platform != "darwin":
        parser.error("native UI e2e requires macOS, a logged-in desktop session, and Xcode")
    try:
        require_unlocked_desktop()
    except (OSError, ValueError, plistlib.InvalidFileException, subprocess.SubprocessError) as exc:
        parser.error(f"desktop preflight failed: {exc}")
    args.output_root.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="run-", dir=args.output_root.resolve()))
    print(f"UI e2e artifacts: {output}", flush=True)
    print(f"UI e2e suite: {args.suite}", flush=True)
    command = [
        "xcodebuild",
        "test",
        "-project",
        str(Path(__file__).parent / "PreviewUI.xcodeproj"),
        "-scheme",
        "PreviewUI",
        "-destination",
        "platform=macOS",
        "-derivedDataPath",
        str(output / "DerivedData"),
        "-resultBundlePath",
        str(output / "Results.xcresult"),
        "-parallel-testing-enabled",
        "NO",
        "-test-timeouts-enabled",
        "YES",
        "-default-test-execution-time-allowance",
        "600",
        "-maximum-test-execution-time-allowance",
        "600",
    ]
    log = output / "xcodebuild.log"
    env = dict(os.environ, TEST_RUNNER_INFERYARD_UI_SUITE=args.suite)
    with log.open("w") as stream:
        started = time.perf_counter()
        result = subprocess.run(
            command, stdout=stream, stderr=subprocess.STDOUT, check=False, env=env
        )
        elapsed = time.perf_counter() - started
    (output / "run.json").write_text(
        json.dumps(
            {
                "suite": args.suite,
                "elapsed_seconds": elapsed,
                "exit_code": result.returncode,
            },
            indent=2,
        )
        + "\n"
    )
    status = "passed" if result.returncode == 0 else "failed"
    print(f"UI e2e ({args.suite}) {status}; build and test: {elapsed:.1f}s", flush=True)
    print(f"Log: {log}\nResult: {output / 'Results.xcresult'}", flush=True)
    if result.returncode:
        lines = log.read_text(errors="replace").splitlines()
        failures = [line for line in lines[:-80] if ": error:" in line]
        print("\n".join([*failures, *lines[-80:]]))
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
