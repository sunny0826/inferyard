"""Launch and stop only instances recorded by this checkout's developer tool."""

from __future__ import annotations

import os
import subprocess
import time
from pathlib import Path

from .build import clean_environment, run, studio_path
from .recovery import validate_proof
from .state import DevError, file_sha, inside, read_json, write_json


def saved_manifest(state: Path, pointer: Path) -> Path | None:
    if not pointer.exists():
        return None
    value = read_json(pointer).get("manifest_path")
    if not isinstance(value, str):
        raise DevError(f"Invalid development instance pointer: {pointer}")
    manifest = inside(Path(value), state / "launches")
    if manifest.name != "manifest.json":
        raise DevError("Invalid development manifest path")
    return manifest


def executable(state: Path, manifest: Path) -> Path:
    # The path itself participates in the launch identity.
    if manifest != inside(manifest, state / "launches") or manifest.name != "manifest.json":
        raise DevError("Invalid development manifest path")
    data = read_json(manifest)
    if data.get("kind") != "studio_dev.v1":
        raise DevError("Unknown development manifest")
    if data.get("checkout") != str(state.parent.resolve()):
        raise DevError("Development instance belongs to another checkout")
    root = data.get("build_root")
    if not isinstance(root, str):
        raise DevError("Missing development build root")
    binary = studio_path(inside(Path(root), state / "builds"))
    if file_sha(binary) != data.get("studio_sha256"):
        raise DevError("Recorded Studio executable has changed; refusing to control the instance")
    return binary


def stop_manifest(state: Path, manifest: Path, *, controller: Path | None = None) -> None:
    deadline = time.monotonic() + 75
    binary = executable(state, manifest)
    if controller is not None:
        control_binary = executable(state, controller)
        hashes = file_sha(controller), file_sha(manifest)
        result = run(
            [
                str(control_binary),
                "--dev-recovery-controller",
                str(controller),
                "--dev-recovery-target",
                str(manifest),
            ],
            cwd=state.parent,
            env=clean_environment(),
            capture=True,
            timeout=max(0, deadline - time.monotonic()),
        )
        outcome = validate_proof(result.stdout, state, controller, manifest, hashes)
        if outcome == "recovered_missing_session":
            return
    run(
        [str(binary), "--dev-stop", str(manifest)],
        cwd=state.parent,
        env=clean_environment(),
        timeout=max(0, deadline - time.monotonic()),
    )


def stop_recorded(state: Path, *, controller: Path | None = None) -> None:
    pending = state / "pending.json"
    paths = [saved_manifest(state, path) for path in (pending, state / "active.json")]
    for manifest in dict.fromkeys(path for path in paths if path is not None):
        stop_manifest(state, manifest, controller=controller)
    pending.unlink(missing_ok=True)


def ready_session(path: Path, session_id: str, cli: Path, pid: int) -> dict | None:
    if not path.exists():
        return None
    value = read_json(path)
    if (
        value.get("kind") != "studio_dev_session.v1"
        or value.get("session_id") != session_id
        or type(value.get("pid")) is not int
        or value["pid"] != pid
    ):
        raise DevError("Studio returned a session belonging to another launch")
    if value.get("state") != "ready":
        return None
    if value.get("cli_path") != str(cli):
        raise DevError("Studio did not bind the prepared CLI")
    return value


def launch(state: Path, manifest: Path, cli: Path, *, timeout: float = 45) -> dict:
    binary = executable(state, manifest)
    directory = manifest.parent
    session_id = directory.name
    pending = state / "pending.json"
    write_json(pending, {"manifest_path": str(manifest)})
    process = None
    try:
        with (directory / "studio.log").open("xb") as log:
            kwargs = (
                {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP | subprocess.DETACHED_PROCESS}
                if os.name == "nt"
                else {
                    "start_new_session": True,
                }
            )
            process = subprocess.Popen(
                [str(binary), "--dev-manifest", str(manifest)],
                cwd=state.parent / "studio",
                env=clean_environment(),
                stdin=subprocess.DEVNULL,
                stdout=log,
                stderr=log,
                **kwargs,
            )
        write_json(directory / "child.json", {"pid": process.pid}, replace=False)
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if process.poll() is not None:
                log_path = directory / "studio.log"
                raise DevError(f"Studio exited before becoming ready; see {log_path}")
            session = ready_session(directory / "session.json", session_id, cli, process.pid)
            if session is not None:
                return session
            time.sleep(0.1)
        raise DevError(f"Studio readiness timed out; see {directory / 'studio.log'}")
    except BaseException:
        if process is not None:
            try:
                stop_manifest(state, manifest, controller=manifest)
            except DevError:
                # Keep pending ownership so the next command cannot hide this instance.
                raise DevError(f"Startup did not shut down cleanly; retained {pending}") from None
        pending.unlink(missing_ok=True)
        raise
