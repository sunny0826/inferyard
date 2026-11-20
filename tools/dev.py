#!/usr/bin/env python3
"""Lightweight checkout development launcher for paired Studio and editable CLI."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import plistlib
import re
import subprocess
import sys
from contextlib import contextmanager
from datetime import UTC, datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from dev_process import (  # noqa: E402
    DevError,
    atomic_write_json,
    is_link,
    launch_studio,
    process_identity,
    stop_recorded,
)

ROOT = Path(__file__).resolve().parents[1]
STATE_DIR_NAME = ".inferyard-dev"

__all__ = [
    "DevError",
    "atomic_write_json",
    "branch_key",
    "check_legacy_active",
    "develop",
    "ensure_workspace",
    "launch_studio",
    "launcher_lock",
    "main",
    "process_identity",
    "resolve_profile",
    "stop_recorded",
    "workspace_ready",
]


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


def _inside(path: Path, root: Path) -> Path | None:
    base = root.resolve()
    try:
        parts = path.absolute().relative_to(base).parts
    except ValueError:
        return None
    if not parts:
        return None
    cursor = base
    for part in parts:
        cursor = cursor / part
        if is_link(cursor):
            return None
    resolved = path.resolve()
    if resolved != base.joinpath(*parts) or not resolved.is_relative_to(base) or resolved == base:
        return None
    return resolved


@contextmanager
def launcher_lock(state_dir: Path):
    state_dir.mkdir(parents=True, exist_ok=True)
    lock_path = state_dir / "launcher.lock"
    if lock_path.is_symlink():
        raise DevError(f"Unsafe launcher lock symlink: {lock_path}")
    with lock_path.open("a+b") as stream:
        if os.name == "nt":
            import msvcrt

            if not lock_path.stat().st_size:
                stream.write(b"\0")
                stream.flush()
            stream.seek(0)
            try:
                msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError as exc:
                raise DevError("Another development launcher command is already running") from exc
        else:
            import fcntl

            try:
                fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError as exc:
                raise DevError("Another development launcher command is already running") from exc
        try:
            yield
        finally:
            if os.name == "nt":
                stream.seek(0)
                msvcrt.locking(stream.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(stream.fileno(), fcntl.LOCK_UN)


def check_legacy_active(state_dir: Path) -> None:
    for name in ("active.json", "pending.json"):
        legacy = state_dir / name
        if legacy.exists() or legacy.is_symlink():
            raise DevError(
                f"Legacy development state found at {legacy}; stop the previous dev instance and remove stale legacy records before continuing"
            )


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
        ["git", "-C", str(root), "rev-parse", "HEAD"],
        capture_output=True,
        text=True,
        check=False,
    )
    if commit.returncode == 0 and commit.stdout.strip():
        return f"detached-{commit.stdout.strip()}"
    return "local"


def branch_label(branch: str) -> str:
    if branch.startswith("detached-") and len(branch) > 19:
        return branch[:19]
    return branch


def branch_key(branch: str) -> str:
    """Match legacy profile key so existing branch profiles continue seamlessly."""
    return hashlib.sha256(branch.encode("utf-8")).hexdigest()[:20]


def studio_version(root: Path) -> str:
    config = root / "studio" / "mygo.config.ts"
    if config.is_file():
        match = re.search(
            r"^\s*version:\s*['\"]([^'\"]+)['\"]", config.read_text(encoding="utf-8"), re.M
        )
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


def build_studio_staged(root: Path, state_dir: Path) -> tuple[Path, Path]:
    """Build into a sibling staged binary so a running executable is untouched."""
    print("Building InferYard Studio...", flush=True)
    final_binary = studio_binary_path(state_dir)
    ensure_macos_bundle(final_binary, studio_version(root))
    staged = final_binary.with_name(f"{final_binary.stem}.next{final_binary.suffix}")
    env = os.environ.copy()
    env.setdefault("GOPROXY", "https://goproxy.cn,direct")
    env["CGO_ENABLED"] = "0"
    env["GOWORK"] = "off"
    env["GOFLAGS"] = "-mod=readonly"
    _run(["go", "build", "-o", str(staged), "."], cwd=root / "studio", env=env)
    _run([str(staged), "--help"], cwd=root, capture=True, timeout=15)
    return staged.resolve(), final_binary


def _safe_subdir(root: Path, *parts: str) -> Path:
    base = root.resolve()
    cursor = base
    for part in parts:
        cursor = cursor / part
        if is_link(cursor):
            raise DevError(f"Refusing to traverse link in development state: {cursor}")
    cursor.mkdir(parents=True, exist_ok=True)
    resolved = cursor.resolve()
    expected = base.joinpath(*parts)
    if resolved != expected or not resolved.is_relative_to(base) or resolved == base:
        raise DevError(f"Development directory escapes expected state path: {cursor}")
    return resolved


def resolve_profile(state_dir: Path, branch: str, *, rebuild: bool) -> tuple[Path, Path]:
    profiles_root = _safe_subdir(state_dir, "profiles", branch_key(branch))
    pointer = profiles_root / "current.json"
    if not rebuild and pointer.is_file() and not pointer.is_symlink():
        try:
            saved = json.loads(pointer.read_text(encoding="utf-8")).get("profile_root")
            if isinstance(saved, str) and saved:
                candidate = _inside(Path(saved), profiles_root)
                if candidate is not None and candidate.is_dir():
                    return candidate, pointer
        except (OSError, ValueError):
            pass
    stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%S%fZ")
    profile = (profiles_root / f"profile-{stamp}").resolve()
    profile.mkdir(parents=True, exist_ok=True)
    return profile, pointer


def workspace_ready(workspace: Path) -> bool:
    prep = workspace / "preparation.json"
    if not prep.is_file() or prep.is_symlink():
        return False
    try:
        data = json.loads(prep.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return False
    return isinstance(data, dict) and data.get("kind") == "community_init.v1"


def ensure_workspace(cli: Path, profile: Path) -> Path:
    workspace = profile / "workspace"
    if workspace_ready(workspace):
        return workspace
    if workspace.exists() or workspace.is_symlink():
        raise DevError(
            f"Development workspace is not initialized: {workspace}; run `mise run dev:rebuild`"
        )
    _run([str(cli), "init", "--out", str(workspace)], cwd=profile, capture=True)
    if not workspace_ready(workspace):
        raise DevError(f"CLI init did not produce a valid workspace: {workspace}")
    return workspace


def develop(root: Path, action: str, *, prepare_only: bool = False) -> None:
    root = root.resolve()
    state_dir = _safe_subdir(root, STATE_DIR_NAME)
    with launcher_lock(state_dir):
        check_legacy_active(state_dir)
        if action == "stop":
            stopped = stop_recorded(state_dir)
            print("Stopped development Studio." if stopped else "No running development Studio recorded.")
            return
        branch = current_branch(root)
        cli = sync_cli(root)
        staged_binary, final_binary = build_studio_staged(root, state_dir)
        assets = _safe_subdir(state_dir, "assets")
        _safe_subdir(state_dir, "assets", "models")
        _safe_subdir(state_dir, "assets", "engines")
        profile, pointer = resolve_profile(state_dir, branch, rebuild=(action == "rebuild"))
        workspace = ensure_workspace(cli, profile)
        if prepare_only:
            print(f"Prepared development build without launching Studio: {staged_binary}")
            print(f"CLI: {cli}")
            print(f"Workspace: {workspace}")
            return
        stop_recorded(state_dir)
        os.replace(staged_binary, final_binary)
        binary = final_binary.resolve()
        label = branch_label(branch)
        session = launch_studio(root, state_dir, binary, cli, profile, assets, label)
        atomic_write_json(pointer, {"profile_root": str(profile)})
        print(f"Development Studio started (PID {session['pid']}) [{label}]")
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
