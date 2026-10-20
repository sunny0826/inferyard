"""Reuse content hashes only while the complete v2 inventory and ctimes agree."""

from pathlib import Path

from inferyard.config.engine_fit_assets import model_asset_manifest
from inferyard.platforms.engine_fit import _stamp
from inferyard.platforms.identity import FileIdentity, PreflightError
from inferyard.platforms.model_assets import inventory

_CACHE = {}


def snapshot(path):
    path = Path(path)
    try:

        def view():
            if path.is_dir():
                return inventory(path)
            return {path.name: (_stamp(path.lstat()), _stamp(path.stat()))}

        before = view()
        cached = _CACHE.get(str(path))
        if cached is not None and cached[0] == before:
            return cached[1], cached[2]
        manifest = model_asset_manifest(path)
        files = []
        for entry in manifest["files"]:
            item = path / entry["path"] if path.is_dir() else path
            value = item.stat()
            files.append(
                FileIdentity(
                    str(item),
                    entry["sha256"],
                    entry["size"],
                    value.st_dev,
                    value.st_ino,
                    value.st_mtime_ns,
                )
            )
        if before != view() or not all(item.unchanged() for item in files):
            raise PreflightError("local_engine_model_changed_during_hash")
        _CACHE[str(path)] = before, manifest, files
        return manifest, files
    except OSError as exc:
        raise PreflightError("local_engine_model_unreadable") from exc
