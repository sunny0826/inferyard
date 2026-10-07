"""Build independent single-file observers; never download or manage model services."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GO_VERSION = "1.27.1"
TOOL_VERSION = "0.2.0"
TARGETS = tuple(
    f"{os_name}/{arch}" for os_name in ("darwin", "linux", "windows") for arch in ("arm64", "amd64")
)


def source_digest() -> str:
    paths = sorted((ROOT / "observer").rglob("*.go"))
    paths += [ROOT / "observer/go.mod", Path(__file__).resolve()]
    digest = hashlib.sha256()
    for path in sorted(paths):
        data = path.read_bytes()
        digest.update(path.relative_to(ROOT).as_posix().encode() + b"\0")
        digest.update(len(data).to_bytes(8, "big") + data)
    return digest.hexdigest()


def build(out: Path, targets: list[str]) -> dict:
    env = {**os.environ, "GOTOOLCHAIN": "local", "GOWORK": "off"}
    version = subprocess.check_output(["go", "version"], env=env, text=True).strip()
    if version.split()[2] != f"go{GO_VERSION}":
        raise ValueError("go_version_does_not_match_mise")
    if any(target.startswith("darwin/") for target in targets) and platform.system() != "Darwin":
        raise ValueError("darwin_build_requires_native_apple_sdk")
    out.mkdir(parents=True, exist_ok=False)
    source_hash = source_digest()
    receipt = {
        "schema_version": 3,
        "kind": "observer_build_receipt",
        "tool_version": TOOL_VERSION,
        "go_version": version,
        "source_sha256": source_hash,
        "source_scope": "observer/**/*.go + observer/go.mod + scripts/build_observer.py",
        "artifacts": [],
        "completed": False,
        "native_validation_granted": False,
    }
    if any(target.startswith("darwin/") for target in targets):
        receipt["c_compiler"] = subprocess.check_output(
            ["clang", "--version"], text=True
        ).splitlines()[0]
    try:
        for target in targets:
            os_name, arch = target.split("/")
            name = f"inferyard-observer-{os_name}-{arch}" + (".exe" if os_name == "windows" else "")
            path = out / name
            build_env = {
                **env,
                "GOOS": os_name,
                "GOARCH": arch,
                "CGO_ENABLED": "1" if os_name == "darwin" else "0",
                "GOAMD64": "v1",
                "GOARM64": "v8.0",
            }
            if os_name == "darwin":
                build_env.update(
                    CC="clang",
                    CGO_CFLAGS="-O2 -g -mmacosx-version-min=13.0",
                    CGO_CPPFLAGS="",
                    CGO_LDFLAGS="-mmacosx-version-min=13.0",
                    MACOSX_DEPLOYMENT_TARGET="13.0",
                )
            subprocess.run(
                [
                    "go",
                    "-C",
                    str(ROOT / "observer"),
                    "build",
                    "-trimpath",
                    "-buildvcs=false",
                    "-ldflags",
                    f"-s -w -X main.version={TOOL_VERSION} -X main.sourceHash={source_hash}",
                    "-o",
                    str(path.resolve()),
                    "./cmd/inferyard-observer",
                ],
                env=build_env,
                check=True,
                timeout=600,
            )
            with path.open("rb") as stream:
                binary_hash = hashlib.file_digest(stream, "sha256").hexdigest()
            receipt["artifacts"].append(
                {
                    "target": target,
                    "file": name,
                    "size_bytes": path.stat().st_size,
                    "sha256": binary_hash,
                    "cgo_enabled": os_name == "darwin",
                    "runtime_requires": "operating_system_libraries_only",
                    "size_budget_met": path.stat().st_size <= 10_000_000,
                    "macos_deployment_target": "13.0" if os_name == "darwin" else None,
                }
            )
        if source_digest() != source_hash:
            raise ValueError("observer_source_changed_during_build")
        receipt["completed"] = True
    finally:
        with (out / "build-receipt.json").open("x", encoding="utf-8") as stream:
            json.dump(receipt, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write("\n")
    return receipt


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True, help="new output directory")
    parser.add_argument("--target", action="append", choices=TARGETS, help="default: all six")
    args = parser.parse_args()
    try:
        receipt = build(args.out, list(dict.fromkeys(args.target or TARGETS)))
    except OSError, ValueError, subprocess.SubprocessError:
        print("observer_build_failed; preserve any partial output and receipt", file=sys.stderr)
        return 4
    print(json.dumps({"completed": receipt["completed"], "artifacts": receipt["artifacts"]}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
