"""Reuse content hashes only while the complete v2 inventory and ctimes agree."""

from __future__ import annotations

import json
import os
import stat
import tempfile
from copy import deepcopy
from pathlib import Path

from inferyard.contracts.json_digest import digest as json_digest
from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import PreflightError
from inferyard.platforms.engine_fit import file_stamp
from inferyard.platforms.identity import FileIdentity
from inferyard.platforms.model_asset_manifest import model_asset_manifest
from inferyard.platforms.model_assets import DEFINITION as MODEL_ASSETS_DEFINITION, inventory

_CACHE = {}
_DISK_CACHE_DEFINITION = "model-hash-cache.v1"
_MAX_DISK_CACHE_BYTES = 4 * 1024 * 1024
_MAX_DISK_CACHE_ENTRIES = 256
_HEX_LOWER = frozenset("0123456789abcdef")


def _hash_cache_path() -> Path:
    return Path.home() / ".cache" / "inferyard" / "model-hash-cache.v1.json"


def _valid_hex64(value: object) -> bool:
    return type(value) is str and len(value) == 64 and set(value) <= _HEX_LOWER


def _load_disk_cache() -> dict:
    try:
        path = _hash_cache_path()
        if path.is_symlink() or (path.parent.exists() and path.parent.is_symlink()):
            return {}
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_size > _MAX_DISK_CACHE_BYTES:
            return {}
        data = strict_json_loads(path.read_text(encoding="utf-8"))
        if (
            not isinstance(data, dict)
            or data.get("definition") != _DISK_CACHE_DEFINITION
            or not isinstance(data.get("entries"), dict)
        ):
            return {}
        return dict(data["entries"])
    except (OSError, ValueError, TypeError, RuntimeError):
        return {}


def _save_disk_cache(entries: dict) -> None:
    tmp_path = None
    try:
        path = _hash_cache_path()
        cache_dir = path.parent
        if path.is_symlink() or (cache_dir.exists() and cache_dir.is_symlink()):
            return
        cache_dir.mkdir(parents=True, exist_ok=True)
        if len(entries) > _MAX_DISK_CACHE_ENTRIES:
            trimmed = dict(list(entries.items())[-_MAX_DISK_CACHE_ENTRIES:])
        else:
            trimmed = entries
        payload = json.dumps(
            {"definition": _DISK_CACHE_DEFINITION, "entries": trimmed},
            ensure_ascii=False,
            allow_nan=False,
            separators=(",", ":"),
        )
        fd, tmp_name = tempfile.mkstemp(
            dir=cache_dir, prefix="model-hash-cache-", suffix=".tmp"
        )
        tmp_path = Path(tmp_name)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(payload)
            stream.flush()
        os.chmod(tmp_path, 0o600)
        os.replace(tmp_path, path)
        tmp_path = None
    except (OSError, ValueError, TypeError, RuntimeError):
        if tmp_path is not None:
            try:
                tmp_path.unlink(missing_ok=True)
            except OSError:
                pass


def _serialize_stamps(stamps: dict) -> dict:
    return {
        name: [list(link), list(target)]
        for name, (link, target) in sorted(stamps.items())
    }


def _deserialize_stamps(raw: object) -> dict | None:
    if not isinstance(raw, dict) or not raw:
        return None
    parsed = {}
    for name, pair in raw.items():
        if type(name) is not str or not isinstance(pair, list) or len(pair) != 2:
            return None
        link, target = pair
        if (
            not isinstance(link, list)
            or not isinstance(target, list)
            or len(link) != 6
            or len(target) != 6
            or any(type(x) is not int for x in link)
            or any(type(x) is not int for x in target)
        ):
            return None
        parsed[name] = (tuple(link), tuple(target))
    return parsed


def _valid_cached_manifest(manifest: object, resolved: str, before: dict, *, is_dir: bool) -> bool:
    if not isinstance(manifest, dict):
        return False
    expected_kind = "directory" if is_dir else "gguf"
    if manifest.get("kind") != expected_kind or manifest.get("path") != resolved:
        return False
    expected_keys = (
        {"kind", "definition", "path", "sha256", "files"}
        if is_dir
        else {"kind", "path", "sha256", "files"}
    )
    if set(manifest) != expected_keys:
        return False
    if is_dir and manifest.get("definition") != MODEL_ASSETS_DEFINITION:
        return False
    if not _valid_hex64(manifest.get("sha256")):
        return False
    files = manifest.get("files")
    if not isinstance(files, list) or not files:
        return False
    expected_sizes = {
        name: target[3]
        for name, (_, target) in before.items()
        if stat.S_ISREG(target[2])
    }
    seen_paths: set[str] = set()
    for entry in files:
        if not isinstance(entry, dict) or set(entry) != {"path", "sha256", "size"}:
            return False
        rel, sha, size = entry["path"], entry["sha256"], entry["size"]
        if (
            type(rel) is not str
            or rel in seen_paths
            or rel not in expected_sizes
            or not _valid_hex64(sha)
            or type(size) is not int
            or size != expected_sizes[rel]
        ):
            return False
        seen_paths.add(rel)
    if seen_paths != set(expected_sizes):
        return False
    return json_digest(files) == manifest["sha256"]


def _lookup_disk_cache(resolved: str, before: dict, *, is_dir: bool) -> dict | None:
    entries = _load_disk_cache()
    entry = entries.get(resolved)
    if not isinstance(entry, dict) or set(entry) != {"stamps", "manifest"}:
        return None
    stamps = _deserialize_stamps(entry.get("stamps"))
    if stamps != before:
        return None
    manifest = entry.get("manifest")
    if not _valid_cached_manifest(manifest, resolved, before, is_dir=is_dir):
        return None
    return manifest


def _record_disk_cache(resolved: str, before: dict, manifest: dict) -> None:
    entries = _load_disk_cache()
    entries.pop(resolved, None)
    entries[resolved] = {
        "stamps": _serialize_stamps(before),
        "manifest": manifest,
    }
    _save_disk_cache(entries)


def _gguf_shard_paths(path: Path) -> list[Path]:
    from inferyard.platforms.asset_scan import parse_gguf_shard_filename

    parsed = parse_gguf_shard_filename(path.name)
    if parsed is None or parsed[1] != 1 or parsed[2] <= 1:
        return [path]
    prefix, _, total = parsed
    shards = [path.parent / f"{prefix}-{idx:05d}-of-{total:05d}.gguf" for idx in range(1, total + 1)]
    return shards if all(p.exists() for p in shards) else [path]


def _file_identities(path: Path, manifest: dict) -> list[FileIdentity]:
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
    return files


def snapshot(path):
    path = Path(path)
    try:
        is_dir = path.is_dir()

        def view():
            if is_dir:
                return inventory(path)
            return {
                shard.name: (file_stamp(shard.lstat()), file_stamp(shard.stat()))
                for shard in _gguf_shard_paths(path)
            }

        before = view()
        cached = _CACHE.get(str(path))
        if cached is not None and cached[0] == before:
            return deepcopy(cached[1]), list(cached[2])

        can_use_disk_cache = not path.is_symlink() and (
            is_dir or not any(stat.S_ISLNK(link[2]) for link, _ in before.values())
        )
        resolved = str(path.resolve(strict=True)) if can_use_disk_cache else None
        manifest = (
            _lookup_disk_cache(resolved, before, is_dir=is_dir)
            if resolved is not None
            else None
        )
        computed = manifest is None
        if computed:
            manifest = model_asset_manifest(path)

        files = _file_identities(path, manifest)
        if before != view() or not all(item.unchanged() for item in files):
            raise PreflightError("local_engine_model_changed_during_hash")
        _CACHE[str(path)] = before, deepcopy(manifest), list(files)
        if computed and resolved is not None:
            _record_disk_cache(resolved, before, manifest)
        return manifest, files
    except OSError as exc:
        raise PreflightError("local_engine_model_unreadable") from exc
