#!/usr/bin/env python3
"""Lightweight checkout development launcher for paired Studio and editable CLI."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import plistlib
import re
import signal
import subprocess
import sys
import time
from datetime import UTC, datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
STATE_DIR_NAME = ".inferyard-dev"


class DevError(RuntimeError):
    pass


def _run(
    args: list[str],
    *,
    cwd: Path,
    env: dict[str, str] | None = None,
    capture: bool = False,
    timeout: int = 600,
) -> subprocess.CompletedProcess[str]:
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
        raise DevError(f"Command failed to run: {args[0]}") from exc
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip() if capture else ""
        suffix = f": {detail}" if detail else ""
        raise DevError(f"Command exited with {result.returncode}: {' '.join(args)}{suffix}")
    return result


def current_branch(root: Path) -> str:
    res = subprocess.run(
        ["git", "-C", str(root), "symbolic-ref", "--quiet", "--short", "HEAD"],
        capture_output=True,
        text=True,
        check=False,
    )
    if res.returncode == 0 and res.stdout.strip():
        return res.stdout.strip()
    commit = subprocess.run(
        ["git", "-C", str(root), "rev-parse", "--short", "HEAD"],
        capture_output=True,
        text=True,
        check=False,
    )
    if commit.returncode == 0 and commit.stdout.strip():
        return f"detached-{commit.stdout.strip()}"
    return "local"


def branch_key(branch: str) -> str:
    slug = re.sub(r"[^A-Za-z0-9._-]+", "-", branch).strip("-") or "branch"
    digest = hashlib.sha256(branch.encode("utf-8")).hexdigest()[:8]
    return f"{slug[:32]}-{digest}"


def studio_version(root: Path) -> str:
    config = root / "studio" / "mygo.config.ts"
    if config.is_file():
        match = re.search(r"^\s*version:\s*['\"]([^'\"]+)['\"]", config.read_text(encoding="utf-8"), re.M)
        if match:
            return match.group(1)
    return "0.0.0-dev"


def studio_binary_path(state_dir: Path) -> Path:
    bin_root = state_dir / "bin"
    if sys.platform == "darwin":
        return bin_root / "InferYard Studio Dev.app" / "Contents" / "MacOS" / "inferyard-studio"
    name = "inferyard-studio.exe" if os.name == "nt" else "inferyard-studio"
    return bin_root / name


def ensure_macos_bundle(binary: Path, version: str) -> None:
    binary.parent.mkdir(parents=True, exist_ok=True)
    if sys.platform != "darwin":
        return
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


def cli_executable(root: Path) -> Path:
    venv = root / "cli" / ".venv"
    if os.name == "nt":
        return venv / "Scripts" / "inferyard.exe"
    return venv / "bin" / "inferyard"


def sync_cli(root: Path) -> Path:
    print("Syncing editable CLI environment (cli/.venv)...", flush=True)
    _run(["uv", "sync", "--frozen"], cwd=root / "cli")
    cli = cli_executable(root).resolve()
    if not cli.is_file():
        raise DevError(f"Expected CLI executable after uv sync: {cli}")
    return cli


def build_studio(root: Path, state_dir: Path) -> Path:
    print("Building InferYard Studio...", flush=True)
    binary = studio_binary_path(state_dir)
    ensure_macos_bundle(binary, studio_version(root))
    env = os.environ.copy()
    env.setdefault("GOPROXY", "https://goproxy.cn,direct")
    env["CGO_ENABLED"] = "0"
    env["GOWORK"] = "off"
    env["GOFLAGS"] = "-mod=readonly"
    _run(["go", "build", "-o", str(binary), "."], cwd=root / "studio", env=env)
    return binary.resolve()


def resolve_profile(state_dir: Path, branch: str, *, rebuild: bool, commit_pointer: bool) -> tuple[Path, Path]:
    profiles_root = state_dir / "profiles" / branch_key(branch)
    profiles_root.mkdir(parents=True, exist_ok=True)
    pointer = profiles_root / "current.json"
    if not rebuild and pointer.is_file():
        try:
            saved = json.loads(pointer.read_text(encoding="utf-8")).get("profile_root")
            if isinstance(saved, str) and saved:
                candidate = Path(saved)
                if candidate.is_dir():
                    return candidate.resolve(), pointer
        except (OSError, ValueError):
            pass
    stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%S%fZ")
    profile = (profiles_root / f"profile-{stamp}").resolve()
    profile.mkdir(parents=True, exist_ok=True)
    if commit_pointer:
        pointer.write_text(json.dumps({"profile_root": str(profile)}, indent=2) + "\n", encoding="utf-8")
    return profile, pointer


def ensure_workspace(cli: Path, profile: Path) -> Path:
    workspace = profile / "workspace"
    if (workspace / "preparation.json").is_file():
        return workspace
    if workspace.exists() and any(workspace.iterdir()):
        raise DevError(f"Development workspace exists without preparation.json: {workspace}; run `mise run dev:rebuild`")
    _run([str(cli), "init", "--out", str(workspace)], cwd=profile, capture=True)
    return workspace


def _process_matches(pid: int, expected_binary: str) -> bool:
    if not expected_binary:
        return False
    if os.name == "nt":
        res = subprocess.run(
            ["tasklist", "/FI", f"PID eq {pid}", "/FO", "CSV", "/NH"],
            capture_output=True,
            text=True,
            check=False,
        )
        exe = Path(expected_binary).name.lower()
        return res.returncode == 0 and f'"{pid}"' in res.stdout and exe in res.stdout.lower()
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    res = subprocess.run(
        ["ps", "-p", str(pid), "-o", "command="],
        capture_output=True,
        text=True,
        check=False,
    )
    if res.returncode != 0:
        return False
    cmd = res.stdout.strip()
    return bool(cmd) and expected_binary in cmd


def stop_recorded(state_dir: Path, *, timeout: float = 30.0) -> bool:
    session_file = state_dir / "session.json"
    if not session_file.is_file():
        return False
    try:
        data = json.loads(session_file.read_text(encoding="utf-8"))
        pid = int(data["pid"])
        binary = str(data.get("binary", ""))
    except (OSError, ValueError, KeyError, TypeError):
        session_file.unlink(missing_ok=True)
        return False
    if not _process_matches(pid, binary):
        session_file.unlink(missing_ok=True)
        return False
    if os.name == "nt":
        subprocess.run(["taskkill", "/PID", str(pid)], capture_output=True, check=False)
    else:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            session_file.unlink(missing_ok=True)
            return True
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if not _process_matches(pid, binary):
            session_file.unlink(missing_ok=True)
            return True
        time.sleep(0.2)
    log_path = data.get("log", "")
    raise DevError(f"Development Studio (PID {pid}) did not exit within {int(timeout)}s; inspect {log_path}")


def launch_studio(
    root: Path, state_dir: Path, binary: Path, cli: Path, profile: Path, assets: Path, label: str
) -> dict[str, object]:
    logs_dir = state_dir / "logs"
    logs_dir.mkdir(parents=True, exist_ok=True)
    log_path = logs_dir / f"studio-{datetime.now(UTC).strftime('%Y%m%dT%H%M%SZ')}.log"
    args = [
        str(binary),
        "--dev-profile",
        str(profile),
        "--dev-assets",
        str(assets),
        "--dev-cli",
        str(cli),
        "--dev-label",
        label,
    ]
    kwargs: dict[str, object] = {"cwd": root}
    if os.name == "nt":
        kwargs["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
    else:
        kwargs["start_new_session"] = True
    with log_path.open("ab") as stream:
        proc = subprocess.Popen(args, stdout=stream, stderr=subprocess.STDOUT, **kwargs)
    record: dict[str, object] = {
        "pid": proc.pid,
        "binary": str(binary),
        "cli": str(cli),
        "profile": str(profile),
        "assets": str(assets),
        "log": str(log_path),
    }
    (state_dir / "session.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    return record


def develop(root: Path, action: str, *, prepare_only: bool = False) -> None:
    root = root.resolve()
    state_dir = (root / STATE_DIR_NAME).resolve()
    state_dir.mkdir(parents=True, exist_ok=True)
    if action == "stop":
        stopped = stop_recorded(state_dir)
        print("Stopped development Studio." if stopped else "No running development Studio recorded.")
        return
    branch = current_branch(root)
    cli = sync_cli(root)
    binary = build_studio(root, state_dir)
    _run([str(binary), "--help"], cwd=root, capture=True, timeout=15)
    assets = (state_dir / "assets").resolve()
    (assets / "models").mkdir(parents=True, exist_ok=True)
    (assets / "engines").mkdir(parents=True, exist_ok=True)
    profile, pointer = resolve_profile(
        state_dir,
        branch,
        rebuild=(action == "rebuild"),
        commit_pointer=not prepare_only,
    )
    workspace = ensure_workspace(cli, profile)
    if prepare_only:
        print(f"Prepared development build without launching Studio: {binary}")
        print(f"CLI: {cli}")
        print(f"Workspace: {workspace}")
        return
    stop_recorded(state_dir)
    session = launch_studio(root, state_dir, binary, cli, profile, assets, branch)
    pointer.write_text(json.dumps({"profile_root": str(profile)}, indent=2) + "\n", encoding="utf-8")
    print(f"Development Studio started (PID {session['pid']}) [{branch}]")
    print(f"CLI: {cli}")
    print(f"Workspace: {workspace}")
    print(f"Log: {session['log']}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", nargs="?", choices=("start", "rebuild", "stop"), default="start")
    parser.add_argument(
        "--prepare-only",
        action="store_true",
        help="Sync CLI, build Studio, and initialize workspace without opening a window",
    )
    args = parser.parse_args(argv)
    if args.prepare_only and args.action == "stop":
        parser.error("--prepare-only cannot be combined with stop")
    try:
        develop(ROOT, args.action, prepare_only=args.prepare_only)
    except (DevError, OSError) as exc:
        print(f"Development launcher: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("Interrupted.", file=sys.stderr)
        return 130
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
