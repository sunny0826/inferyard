"""Consume the same downloaded candidate in a disposable OS matrix, without rebuilding."""

import argparse
import json
import os
import shutil
import subprocess
from pathlib import Path

from run_installed import artifact_files, environment, read_json, run_matrix


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifact", type=Path, required=True)
    parser.add_argument("--inputs", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    manifest_path = args.artifact / "manifest.json"
    manifest = read_json(manifest_path.read_bytes())
    snapshots = artifact_files(args.artifact, manifest)
    files = {role: args.artifact / file.path for role, file in snapshots.items()}
    run_matrix(files["wheel"], files["constraints"], args.inputs.resolve(), args.out, manifest_path)
    out = args.out.resolve()
    env = environment(out)
    version = json.loads((args.inputs / "expected.json").read_text(encoding="utf-8"))[
        "pytest_version"
    ]
    # Test-only environment: runtime pins stay fixed; pytest is the frozen repository test version.
    env.update(UV_TOOL_DIR=str(out / "tools-test"), UV_TOOL_BIN_DIR=str(out / "bin-test"))
    subprocess.run(
        [
            "uv",
            "tool",
            "install",
            "--managed-python",
            "--python",
            "3.14.7",
            "--constraints",
            str(files["constraints"]),
            "--with",
            f"pytest=={version}",
            str(files["wheel"]),
        ],
        env=env,
        check=True,
    )
    test_python = (
        out / "tools-test/inferyard" / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    )
    request_script = out / "inputs/installed_request_chain.py"
    shutil.copyfile(Path(__file__).with_name("installed_request_chain.py"), request_script)
    script = args.out / "inputs/installed_host_state.py"
    shutil.copyfile(Path(__file__).with_name("installed_host_state.py"), script)

    def request_check(mode):
        subprocess.run(
            [
                str(test_python),
                "-I",
                str(request_script),
                "--fixtures",
                str(out / "inputs/fixtures"),
                "--mode",
                mode,
                "--out",
                str(out / f"request-chain-{mode}.json"),
            ],
            cwd=out / "empty-cwd",
            env=env,
            check=True,
            timeout=60,
        )

    request_check("success")
    python = (
        args.out / "tools/inferyard" / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    )
    subprocess.run(
        [str(python), "-I", str(script), "--out", str(args.out / "host-state.json")],
        cwd=args.out / "empty-cwd",
        env=environment(args.out),
        check=True,
        timeout=60,
    )
    request_check("dirty")


if __name__ == "__main__":
    main()
