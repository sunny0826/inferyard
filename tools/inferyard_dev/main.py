"""One-command paired builds and non-destructive development profiles."""

from __future__ import annotations

import argparse
import secrets
import sys
from pathlib import Path

from .build import Build, Toolchain, clean_environment, prepare, run
from .lifecycle import launch, stop_recorded
from .state import (
    DevError,
    Source,
    inside,
    launcher_lock,
    private_dir,
    read_json,
    source,
    write_json,
)


def profile_for(state: Path, info: Source, rebuild: bool) -> tuple[Path, Path]:
    branch_root = private_dir(inside(state / "profiles" / info.profile_key, state))
    pointer = branch_root / "current.json"
    if pointer.exists() and not rebuild:
        value = read_json(pointer).get("profile_root")
        if not isinstance(value, str):
            raise DevError("Invalid development profile pointer; existing files were preserved")
        return inside(Path(value), branch_root), pointer
    return private_dir(branch_root / secrets.token_hex(16)), pointer


def initialize(profile: Path, build: Build) -> None:
    workspace = profile / "workspace"
    if workspace.exists():
        preparation = workspace / "preparation.json"
        if not preparation.is_file() or read_json(preparation).get("kind") != "community_init.v1":
            raise DevError("Development workspace is not initialized; use mise run dev:rebuild")
        return
    run(
        [str(build.cli), "init", "--out", str(workspace)],
        cwd=build.root,
        env=clean_environment(),
        capture=True,
    )


def manifest_for(state: Path, info: Source, build: Build, profile: Path) -> Path:
    assets = private_dir(inside(state / "assets", state))
    private_dir(assets / "models")
    private_dir(assets / "engines")
    directory = private_dir(inside(state / "launches" / secrets.token_hex(16), state))
    path = directory / "manifest.json"
    write_json(
        path,
        {
            "kind": "studio_dev.v1",
            **info.fields(),
            "build_root": str(build.root),
            "profile_root": str(profile),
            "assets_root": str(assets),
            "package_root": str(build.package),
            **build.hashes,
        },
        replace=False,
    )
    return path


def develop(
    root: Path, action: str, *, prepare_only: bool = False, controller_manifest: Path | None = None
) -> Path | None:
    if controller_manifest is not None and action != "stop":
        raise DevError("--controller-manifest is only valid with stop")
    root = root.resolve()
    state = root / ".inferyard-dev"
    with launcher_lock(state):
        if action == "stop":
            stop_recorded(state, controller=controller_manifest)
            (state / "active.json").unlink(missing_ok=True)
            print("The recorded development Studio has shut down.")
            return None
        info = source(root)
        tools = Toolchain.load(root)
        output = inside(state / "builds" / secrets.token_hex(16), state)
        build = prepare(root, output, info, tools)
        profile, pointer = profile_for(state, info, action == "rebuild")
        initialize(profile, build)
        manifest = manifest_for(state, info, build, profile)
        run(
            [str(build.studio), "--dev-check", str(manifest)],
            cwd=root,
            env=clean_environment(),
            capture=True,
            timeout=30,
        )
        if source(root) != info:
            raise DevError("Checkout inputs changed during preparation; run the command again")
        if prepare_only:
            print(f"Prepared and checked, without opening a window: {manifest}")
            return manifest
        stop_recorded(state, controller=manifest)
        session = launch(state, manifest, build.cli)
        write_json(pointer, {"profile_root": str(profile)})
        write_json(state / "active.json", {"manifest_path": str(manifest)})
        (state / "pending.json").unlink(missing_ok=True)
        suffix = " + modified files" if info.dirty else ""
        print(f"Development Studio ready: {info.branch} @ {info.commit[:10]}{suffix}")
        print(f"Studio PID: {session['pid']}")
        print(f"CLI: {build.cli}")
        print(f"Workspace: {profile / 'workspace'}")
        print(f"Session: {manifest.parent / 'session.json'}")
        return manifest


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", nargs="?", choices=("start", "rebuild", "stop"), default="start")
    parser.add_argument(
        "--prepare-only",
        action="store_true",
        help="Build, initialize and check without opening Studio or switching profiles",
    )
    parser.add_argument(
        "--controller-manifest",
        type=Path,
        help="Use a prepared controller to check an older missing-session launch (stop only)",
    )
    args = parser.parse_args(argv)
    if args.prepare_only and args.action == "stop":
        parser.error("--prepare-only cannot be combined with stop")
    if args.controller_manifest is not None and args.action != "stop":
        parser.error("--controller-manifest is only valid with stop")
    try:
        develop(
            Path(__file__).resolve().parents[2],
            args.action,
            prepare_only=args.prepare_only,
            controller_manifest=args.controller_manifest,
        )
    except (DevError, OSError) as exc:
        print(f"Development environment: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("Development preparation interrupted; history was preserved.", file=sys.stderr)
        return 130
    return 0
