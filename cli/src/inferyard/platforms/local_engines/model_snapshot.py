"""Reuse content hashes only while the complete v2 inventory and ctimes agree."""

from pathlib import Path

from inferyard.platforms.model_asset_manifest import model_asset_manifest
from inferyard.errors import PreflightError
from inferyard.platforms.engine_fit import file_stamp
from inferyard.platforms.identity import FileIdentity
from inferyard.platforms.model_assets import inventory

_CACHE = {}


def _gguf_shard_paths(path: Path) -> list[Path]:
    from inferyard.platforms.asset_scan import parse_gguf_shard_filename

    parsed = parse_gguf_shard_filename(path.name)
    if parsed is None or parsed[1] != 1 or parsed[2] <= 1:
        return [path]
    prefix, _, total = parsed
    shards = [path.parent / f"{prefix}-{idx:05d}-of-{total:05d}.gguf" for idx in range(1, total + 1)]
    return shards if all(p.exists() for p in shards) else [path]


def snapshot(path):
    path = Path(path)
    try:

        def view():
            if path.is_dir():
                return inventory(path)
            return {
                shard.name: (file_stamp(shard.lstat()), file_stamp(shard.stat()))
                for shard in _gguf_shard_paths(path)
            }

        before = view()
        cached = _CACHE.get(str(path))
        if cached is not None and cached[0] == before:
            return cached[1], cached[2]
        manifest = model_asset_manifest(path)
        files = []
        multi_shard = not path.is_dir() and len(manifest["files"]) > 1
        for entry in manifest["files"]:
            if path.is_dir():
                item = path / entry["path"]
            elif multi_shard:
                item = path.parent / entry["path"]
            else:
                item = path
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
