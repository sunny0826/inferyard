"""Portable diagnostic model assets and the installed controlled MLX entrypoint."""

import hashlib
from importlib.resources import files
from pathlib import Path

from inferyard.platforms.identity import PreflightError


def mlx_server_path():
    """Return the installed script, never a source-checkout-relative path."""
    return Path(str(files("inferyard").joinpath("data/engine_fit/mlx_server.py")))


def mlx_server_sha256():
    return hashlib.sha256(mlx_server_path().read_bytes()).hexdigest()


def runtime_manifest(plan):
    """Resolve the frozen asset algorithm, without importing the live runner."""
    from inferyard.platforms.engine_fit import model_manifest

    path = Path(plan["model"]["path"])
    if plan["definition"] != "engine_fit_plan.v1":
        return model_asset_manifest(path, definition=plan["model"].get("definition"))
    return model_manifest(path)


def model_asset_manifest(path, *, definition="model-assets.v2"):
    from inferyard.platforms.engine_fit import _file_hash, _stamp, model_manifest
    from inferyard.platforms.gguf_metadata import read_metadata

    path = Path(path)
    if path.is_symlink():
        raise PreflightError("engine_fit_directory_symlink")
    if path.is_dir():
        from inferyard.evidence.formats import UnsupportedFormat, require_version

        if definition is None:
            raise UnsupportedFormat("model-assets", "unversioned", ("model-assets.v2",))

        require_version(
            {"definition": definition}, "definition", ("model-assets.v2",), "model-assets"
        )
        return {"kind": "directory", **model_manifest(path, definition=definition)}
    try:
        root = path.resolve(strict=True)
        if not root.is_file() or root.suffix.lower() != ".gguf":
            raise PreflightError("engine_fit_single_gguf_required")
        before = _stamp(root.stat())
        sha, size = _file_hash(root)
        metadata = read_metadata(root, extra_keys=("split.count", "split.no"))
        count, index = metadata.get("split.count"), metadata.get("split.no")
        if (count is not None and (type(count) is not int or count != 1)) or (
            index is not None and (type(index) is not int or index != 0)
        ):
            raise PreflightError("engine_fit_split_gguf_unsupported")
        if _stamp(root.stat()) != before:
            raise PreflightError("engine_fit_model_changed_during_hash")
        entries = [{"path": root.name, "sha256": sha, "size": size}]
        from inferyard.config.engine_fit import digest

        return {"kind": "gguf", "path": str(root), "sha256": digest(entries), "files": entries}
    except (OSError, ValueError) as exc:
        raise PreflightError("engine_fit_model_unreadable") from exc
