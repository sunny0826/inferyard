"""Build one non-release candidate and independently rebuild its sdist; no upload."""

import argparse
import subprocess
import sys
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT).strip()
    if subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT):
        raise ValueError("distribution_requires_clean_source")
    out = args.out.resolve()
    out.mkdir(parents=True)
    for folder in ("packages", "rebuilt", "attachments"):
        (out / folder).mkdir()
    version = tomllib.loads((ROOT / "pyproject.toml").read_text(encoding="utf-8"))["project"][
        "version"
    ]
    wheel = f"inferyard-{version}-py3-none-any.whl"
    sdist = f"inferyard-{version}.tar.gz"
    constraints = out / "attachments/runtime-constraints.txt"
    commands = [
        [sys.executable, "scripts/export_runtime_constraints.py", "--out", str(constraints)],
        ["uv", "build", "--no-sources", "--out-dir", str(out / "packages")],
        [
            "uv",
            "build",
            "--no-sources",
            "--wheel",
            str(out / "packages" / sdist),
            "--out-dir",
            str(out / "rebuilt"),
        ],
        [
            sys.executable,
            "scripts/check_distribution.py",
            "--wheel",
            str(out / "packages" / wheel),
            "--sdist",
            str(out / "packages" / sdist),
            "--constraints",
            str(constraints),
            "--rebuilt-wheel",
            str(out / "rebuilt" / wheel),
            "--out",
            str(out / "manifest.json"),
        ],
    ]
    for command in commands:
        subprocess.run(command, cwd=ROOT, check=True)
        if subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT
        ).strip() != commit or subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT):
            (out / "manifest.json").unlink(missing_ok=True)
            raise ValueError("distribution_source_changed_during_build")


if __name__ == "__main__":
    main()
