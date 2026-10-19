"""Prepare immutable paired build outputs from this checkout's locked inputs."""

from __future__ import annotations

import json
import os
import platform
import plistlib
import subprocess
import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path

from .state import DevError, Source, file_sha, inside, package_sha, private_dir, source, write_json


def clean_environment() -> dict[str, str]:
    env = os.environ.copy()
    for key in (
        "PYTHONPATH",
        "PYTHONHOME",
        "VIRTUAL_ENV",
        "UV_PROJECT_ENVIRONMENT",
        "UV_PROJECT",
        "UV_WORKING_DIRECTORY",
    ):
        env.pop(key, None)
    env["PYTHONNOUSERSITE"] = "1"
    env["PYTHONUTF8"] = "1"
    env["UV_PYTHON_DOWNLOADS"] = "never"
    return env


def run(args: list[str], *, cwd: Path, env=None, capture=False, timeout=600):
    try:
        result = subprocess.run(
            args,
            cwd=cwd,
            env=env,
            check=False,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=capture,
            timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise DevError(f"Development command could not complete: {Path(args[0]).name}") from exc
    if result.returncode:
        raise DevError(f"Development command failed ({result.returncode}): {Path(args[0]).name}")
    return result


@dataclass(frozen=True)
class Toolchain:
    python: str
    uv: str
    go: str
    versions: dict[str, str]

    @classmethod
    def load(cls, root: Path) -> Toolchain:
        versions = tomllib.loads((root / "mise.toml").read_text(encoding="utf-8"))["tools"]
        paths = {
            name: run(["mise", "which", name], cwd=root, capture=True).stdout.strip()
            for name in ("python", "uv", "go")
        }
        checks = {"python": ["--version"], "uv": ["--version"], "go": ["version"]}
        for name, args in checks.items():
            version = run([paths[name], *args], cwd=root, capture=True).stdout.split()
            expected = f"go{versions[name]}" if name == "go" else versions[name]
            if expected not in version:
                raise DevError(f"mise {name} does not match mise.toml")
        if Path(sys.executable).resolve() != Path(paths["python"]).resolve():
            raise DevError("Run this command with mise's Python")
        return cls(**paths, versions={name: versions[name] for name in paths})


@dataclass(frozen=True)
class Build:
    root: Path
    studio: Path
    python: Path
    cli: Path
    package: Path
    wheel: Path
    hashes: dict[str, str]


def studio_path(output: Path) -> Path:
    if sys.platform == "darwin":
        return output / "InferYard Studio Dev.app/Contents/MacOS/inferyard-studio"
    return output / ("inferyard-studio.exe" if os.name == "nt" else "inferyard-studio")


def prepare_native_bundle(output: Path, version: str) -> Path:
    binary = studio_path(output)
    private_dir(binary.parent)
    if sys.platform == "darwin":
        plist = {
            "CFBundleExecutable": binary.name,
            "CFBundleIdentifier": "local.inferyard.studio.dev",
            "CFBundleName": "InferYard Studio Dev",
            "CFBundleDisplayName": "InferYard Studio Dev",
            "CFBundlePackageType": "APPL",
            "CFBundleShortVersionString": version,
            "CFBundleVersion": version,
            "LSMinimumSystemVersion": "12.0",
            "NSHighResolutionCapable": True,
        }
        (binary.parent.parent / "Info.plist").write_bytes(plistlib.dumps(plist))
    return binary


def prepare(root: Path, output: Path, info: Source, tools: Toolchain) -> Build:
    private_dir(output)
    env = clean_environment()
    environment = output / "cli-env"
    env["UV_PROJECT_ENVIRONMENT"] = str(environment)
    print("Synchronizing the development CLI's locked dependencies…", flush=True)
    run(
        [
            tools.uv,
            "sync",
            "--frozen",
            "--no-install-project",
            "--no-dev",
            "--python",
            tools.python,
        ],
        cwd=root / "cli",
        env=env,
    )
    print("Building the CLI from the current checkout…", flush=True)
    wheels = private_dir(output / "wheels")
    run([tools.uv, "build", "--wheel", "--out-dir", str(wheels)], cwd=root / "cli", env=env)
    candidates = list(wheels.glob("inferyard-*.whl"))
    if len(candidates) != 1:
        raise DevError("Expected one locally built InferYard wheel")
    wheel = candidates[0]
    bin_dir = environment / ("Scripts" if os.name == "nt" else "bin")
    python = bin_dir / ("python.exe" if os.name == "nt" else "python")
    cli = bin_dir / ("inferyard.exe" if os.name == "nt" else "inferyard")
    run(
        [
            tools.uv,
            "pip",
            "install",
            "--python",
            str(python),
            "--no-deps",
            "--reinstall",
            str(wheel),
        ],
        cwd=root / "cli",
        env=env,
    )
    query = (
        "import inferyard,json,pathlib; "
        "print(json.dumps(str(pathlib.Path(inferyard.__file__).resolve().parent)))"
    )
    package = inside(
        Path(
            json.loads(
                run(
                    [str(python), "-I", "-X", "utf8", "-c", query],
                    cwd=output,
                    env=env,
                    capture=True,
                ).stdout
            )
        ),
        environment,
    )
    installed = run([str(cli), "--version"], cwd=output, env=env, capture=True).stdout.strip()
    project = tomllib.loads((root / "cli/pyproject.toml").read_text(encoding="utf-8"))
    version = project["project"]["version"]
    if installed != f"inferyard {version} source=release":
        raise DevError("The development CLI is not the freshly installed local package")
    print("Building the native Studio development executable…", flush=True)
    studio = prepare_native_bundle(output, version)
    env.update(
        GOTOOLCHAIN=f"go{tools.versions['go']}",
        GOPROXY="https://goproxy.cn,direct",
        CGO_ENABLED="0",
        GOWORK="off",
        GOFLAGS="-mod=readonly",
    )
    host = run(
        [tools.go, "env", "GOHOSTOS", "GOHOSTARCH"], cwd=root / "studio", env=env, capture=True
    ).stdout.split()
    env.update(GOOS=host[0], GOARCH=host[1])
    run(
        [tools.go, "build", "-tags", "studio_dev", "-o", str(studio), "."],
        cwd=root / "studio",
        env=env,
    )
    if source(root) != info:
        raise DevError("Checkout inputs changed during the build; run the command again")
    hashes = {
        "studio_sha256": file_sha(studio),
        "cli_sha256": file_sha(cli),
        "package_sha256": package_sha(package),
    }
    write_json(
        output / "build.json",
        {
            **info.fields(),
            **hashes,
            "wheel_sha256": file_sha(wheel),
            "toolchain": tools.versions,
            "platform": platform.platform(),
        },
        replace=False,
    )
    return Build(output, studio, python, cli, package, wheel, hashes)
