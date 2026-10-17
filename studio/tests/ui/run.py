"""Run macOS preview UI tests; keep each run's logs and xcresult in a new directory."""

from __future__ import annotations

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-root", type=Path, default=Path("build/ui-e2e"))
    args = parser.parse_args()
    if sys.platform != "darwin":
        parser.error("native UI e2e requires macOS, a logged-in desktop session, and Xcode")
    args.output_root.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="run-", dir=args.output_root.resolve()))
    print(f"UI e2e artifacts: {output}", flush=True)
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
        "60",
        "-maximum-test-execution-time-allowance",
        "90",
    ]
    log = output / "xcodebuild.log"
    with log.open("w") as stream:
        result = subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, check=False)
    print("UI e2e passed" if result.returncode == 0 else "UI e2e failed", flush=True)
    print(f"Log: {log}\nResult: {output / 'Results.xcresult'}", flush=True)
    if result.returncode:
        print("\n".join(log.read_text(errors="replace").splitlines()[-80:]))
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
