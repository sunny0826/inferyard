"""Explicit workspace defaults shared by candidate preparation and progress."""

from dataclasses import replace
from pathlib import Path

from inferyard.config.preparation_io import PreparationError, read_json


def latest(paths):
    return max(paths, key=lambda p: (p.stat().st_mtime_ns, str(p)), default=None)


def preflight_files(workspace):
    root = workspace / "preflight"
    if not root.exists():
        return []
    if not root.is_dir():
        raise PreparationError("workspace_artifact_unreadable")
    return list(root.rglob("device-preflight.json"))


def latest_preflight(workspace):
    return latest(preflight_files(workspace))


def resolve_candidate_inputs(request):
    if request.workspace is None:
        if request.preflight is None or request.output_root is None:
            raise PreparationError("invalid_input")
        return request
    try:
        workspace = request.workspace.resolve(strict=True)
        if not workspace.is_dir():
            raise PreparationError("invalid_workspace")
        bundle = request.bundle_path
        if bundle is not None and not bundle.is_absolute() and bundle.parent == Path("."):
            if not bundle.suffix:
                bundle = workspace / "bundles" / (bundle.name + ".json")
        preflight = request.preflight or latest_preflight(workspace)
        if preflight is None:
            raise PreparationError("device_preflight_missing")
        read_json(preflight)
        read_json(bundle)
        return replace(
            request,
            preflight=preflight,
            bundle_path=bundle,
            output_root=request.output_root or workspace / "results",
        )
    except OSError, TypeError:
        raise PreparationError("workspace_artifact_unreadable") from None
