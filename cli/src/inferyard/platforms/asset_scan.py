"""Read-only discovery of local GGUF models, directory models, and engine binaries."""

from __future__ import annotations

import errno
import json
import os
import re
import shutil
import stat
from collections.abc import Iterator
from pathlib import Path

from inferyard.platforms.gguf_metadata import read_metadata

MODEL_SCAN_DEPTH = 4
ENGINE_SCAN_DEPTH = 4
SKIP_DIRECTORIES = frozenset({"venv", "env", "node_modules", "site-packages", "__pycache__"})
ENGINE_BINARY_NAMES = frozenset({"llama-server", "llama-server.exe"})
GGUF_SHARD_PATTERN = re.compile(
    r"^(?P<prefix>.+)-(?P<index>\d{5})-of-(?P<total>\d{5})\.gguf$", re.IGNORECASE
)


def parse_gguf_shard_filename(name: str) -> tuple[str, int, int] | None:
    """Return (prefix, 1-based shard index, shard total) for *-NNNNN-of-MMMMM.gguf."""
    match = GGUF_SHARD_PATTERN.fullmatch(name)
    if match is None:
        return None
    index = int(match.group("index"))
    total = int(match.group("total"))
    if total < 2 or not (1 <= index <= total):
        return None
    return match.group("prefix"), index, total


def _safe_resolve_text(path: Path) -> str:
    try:
        return str(path.resolve())
    except OSError:
        return str(path.absolute())


def inspect_gguf_file(
    path: Path | str, *, metadata_reader=read_metadata, fallback_name: str | None = None
) -> dict:
    """Inspect one GGUF file without loading tensors or running templates."""
    candidate = Path(path)
    resolved = _safe_resolve_text(candidate)
    try:
        file_stat = candidate.stat()
        if not stat.S_ISREG(file_stat.st_mode):
            raise ValueError("non_regular_gguf")
        metadata = metadata_reader(candidate)
        architecture = metadata.get("general.architecture")
        for key in (
            "general.name",
            "general.architecture",
            "tokenizer.chat_template",
            "tokenizer.chat_template.default",
        ):
            if metadata.get(key) is not None and not isinstance(metadata[key], str):
                raise ValueError("invalid_gguf_metadata_type")
        context = metadata.get(str(architecture) + ".context_length")
        if context is not None and (type(context) is not int or context <= 0):
            raise ValueError("invalid_gguf_context_length")
        file_type = metadata.get("general.file_type")
        if file_type is not None and (type(file_type) is not int or file_type < 0):
            raise ValueError("invalid_gguf_file_type")
        return {
            "path": resolved,
            "name": metadata.get("general.name") or fallback_name or candidate.stem,
            "size_bytes": file_stat.st_size,
            "architecture": architecture,
            "context_length": context,
            "file_type": file_type,
            "status": "available",
            "chat_template_available": bool(
                metadata.get("tokenizer.chat_template")
                or metadata.get("tokenizer.chat_template.default")
            ),
        }
    except OSError, ValueError, UnicodeError:
        return {
            "path": resolved,
            "status": "unavailable",
            "reason": "invalid_or_unreadable_gguf",
        }


def _hf_hub_cache_roots() -> tuple[Path, ...]:
    candidates: list[Path] = []
    if hub_env := os.environ.get("HF_HUB_CACHE"):
        candidates.append(Path(hub_env))
    if hf_home := os.environ.get("HF_HOME"):
        candidates.append(Path(hf_home) / "hub")
    candidates.append(Path.home() / ".cache" / "huggingface" / "hub")
    roots: list[Path] = []
    for candidate in candidates:
        try:
            resolved = candidate.resolve(strict=True)
            if resolved.is_dir():
                roots.append(resolved)
        except OSError:
            continue
    return tuple(dict.fromkeys(roots))


def _allowed_roots_for_dir(
    current_dir: Path, root_real: Path, hub_roots: tuple[Path, ...]
) -> tuple[Path, ...]:
    allowed = [root_real, current_dir, *hub_roots]
    for ancestor in (current_dir, *current_dir.parents):
        if not ancestor.name.startswith("models--"):
            continue
        snapshots = ancestor / "snapshots"
        if current_dir == snapshots or current_dir.is_relative_to(snapshots):
            blobs = ancestor / "blobs"
            try:
                resolved_blobs = blobs.resolve(strict=True)
                if resolved_blobs.is_dir():
                    allowed.append(resolved_blobs)
            except OSError:
                pass
            allowed.append(ancestor)
            break
    return tuple(dict.fromkeys(allowed))


def _resolve_controlled_file(entry_path: Path, allowed_roots: tuple[Path, ...]) -> Path | None:
    try:
        lstat = entry_path.lstat()
    except OSError:
        return None
    if stat.S_ISREG(lstat.st_mode):
        return entry_path
    if not stat.S_ISLNK(lstat.st_mode):
        return None
    try:
        target = entry_path.resolve(strict=True)
        target_stat = target.stat()
    except OSError, RuntimeError:
        return None
    if not stat.S_ISREG(target_stat.st_mode):
        return None
    if any(target.is_relative_to(allowed) for allowed in allowed_roots):
        return target
    return None


def _skip_dir(name: str) -> bool:
    return name.startswith(".") or name in SKIP_DIRECTORIES


def is_polystrata_auxiliary_gguf(path: Path) -> bool:
    """Return True for PolyStrata MTP draft or pack GGUFs that are not standalone models."""
    if path.parent.name.lower() == "mtp" and path.name.lower().startswith("mtp-"):
        return True
    parts_lower = tuple(part.lower() for part in path.parts)
    for idx, part in enumerate(parts_lower[:-1]):
        if part == "polystrata-data" and idx + 1 < len(parts_lower) - 1:
            if parts_lower[idx + 1] in ("mtp", "packs"):
                return True
    return False


def _walk_bounded(root: Path, max_depth: int) -> Iterator[tuple[Path, Path, list[os.DirEntry]]]:
    try:
        root.lstat()
    except FileNotFoundError:
        return
    root_real = root.resolve(strict=True)
    if not root_real.is_dir():
        raise NotADirectoryError(errno.ENOTDIR, "not a directory")
    pending: list[tuple[Path, int]] = [(root_real, 0)]
    while pending:
        current_dir, depth = pending.pop(0)
        try:
            with os.scandir(current_dir) as it:
                entries = sorted(it, key=lambda entry: entry.name)
        except OSError:
            if depth == 0:
                raise
            continue
        files: list[os.DirEntry] = []
        for entry in entries:
            try:
                is_symlink = entry.is_symlink()
                is_dir = False if is_symlink else entry.is_dir(follow_symlinks=False)
            except OSError:
                continue
            if is_dir:
                if depth < max_depth and not _skip_dir(entry.name):
                    pending.append((current_dir / entry.name, depth + 1))
                continue
            files.append(entry)
        yield root_real, current_dir, files


def _read_config_summary(config_path: Path | None) -> tuple[str | None, int | None]:
    if config_path is None:
        return None, None
    try:
        data = json.loads(config_path.read_text(encoding="utf-8"))
    except OSError, UnicodeError, ValueError:
        return None, None
    if not isinstance(data, dict):
        return None, None
    architecture = None
    model_type = data.get("model_type")
    if isinstance(model_type, str) and model_type.strip():
        architecture = model_type.strip()
    else:
        arches = data.get("architectures")
        if isinstance(arches, list) and arches and isinstance(arches[0], str) and arches[0].strip():
            architecture = arches[0].strip()
    context_length = data.get("max_position_embeddings")
    if type(context_length) is not int or context_length <= 0:
        context_length = None
    return architecture, context_length


def _has_chat_template(tokenizer_config_path: Path | None) -> bool:
    if tokenizer_config_path is None:
        return False
    try:
        data = json.loads(tokenizer_config_path.read_text(encoding="utf-8"))
    except OSError, UnicodeError, ValueError:
        return False
    if not isinstance(data, dict):
        return False
    value = data.get("chat_template")
    return isinstance(value, str) and bool(value.strip())


def _gguf_asset_from_info(info: dict, stem: str) -> dict:
    if info["status"] == "available":
        chat_ok = bool(info.get("chat_template_available"))
        return {
            "path": info["path"],
            "name": info["name"],
            "format": "gguf",
            "size_bytes": info["size_bytes"],
            "architecture": info["architecture"],
            "context_length": info["context_length"],
            "available": chat_ok,
            "reason": None if chat_ok else "chat_template_unavailable",
        }
    return {
        "path": info["path"],
        "name": info.get("name") or stem,
        "format": "gguf",
        "size_bytes": info.get("size_bytes"),
        "architecture": None,
        "context_length": None,
        "available": False,
        "reason": info.get("reason") or "invalid_or_unreadable_gguf",
    }


def _gguf_asset(path: Path, stem: str) -> dict:
    return _gguf_asset_from_info(inspect_gguf_file(path, fallback_name=stem), stem)


def inspect_gguf_entries(paths: list[Path], *, metadata_reader=read_metadata) -> list[dict]:
    """Inspect GGUF files, collapsing multi-shard sets per directory into single model entries."""
    standalone: list[Path] = []
    groups: dict[tuple[str, str, int], dict[int, Path]] = {}
    for candidate in paths:
        parsed = parse_gguf_shard_filename(candidate.name)
        if parsed is None:
            standalone.append(candidate)
            continue
        prefix, index, total = parsed
        key = (_safe_resolve_text(candidate.parent), prefix, total)
        groups.setdefault(key, {})[index] = candidate

    results = [inspect_gguf_file(p, metadata_reader=metadata_reader) for p in standalone]
    for (_dir_key, prefix, total), shards in sorted(groups.items()):
        primary_idx = 1 if 1 in shards else min(shards)
        primary = shards[primary_idx]
        total_size = 0
        all_readable = True
        for idx in range(1, total + 1):
            shard_path = shards.get(idx)
            if shard_path is None:
                all_readable = False
                continue
            try:
                st = shard_path.stat()
                if not stat.S_ISREG(st.st_mode):
                    all_readable = False
                    continue
                total_size += int(st.st_size)
            except OSError:
                all_readable = False
        if not all_readable or 1 not in shards or len(shards) != total:
            results.append(
                {
                    "path": _safe_resolve_text(primary),
                    "name": prefix,
                    "size_bytes": total_size if total_size > 0 else None,
                    "status": "unavailable",
                    "reason": "split_gguf_incomplete",
                }
            )
            continue
        info = inspect_gguf_file(primary, metadata_reader=metadata_reader, fallback_name=prefix)
        if info["status"] == "available":
            info = {**info, "size_bytes": total_size}
        else:
            info = {**info, "name": prefix}
        results.append(info)
    return results


def _disk_free_bytes(root: Path) -> int | None:
    try:
        resolved = root.resolve(strict=True)
        if not resolved.is_dir():
            return None
        return int(shutil.disk_usage(resolved).free)
    except OSError:
        return None


def _unique_roots(raw_roots) -> list[tuple[Path, str]]:
    items: list[tuple[Path, str]] = []
    seen: set[str] = set()
    for raw in raw_roots or ():
        if not raw:
            continue
        text = str(raw)
        if not text.strip():
            continue
        path = Path(text)
        key = _safe_resolve_text(path)
        if key in seen:
            continue
        seen.add(key)
        items.append((path, key))
    return items


def _scan_models_root(
    root: Path,
    hub_roots: tuple[Path, ...],
    seen_paths: set[str],
    models: list[dict],
    model_sizes: dict[str, int],
) -> int:
    usage = 0
    counted: set[str] = set()
    for root_real, current_dir, entries in _walk_bounded(root, MODEL_SCAN_DEPTH):
        allowed_roots = _allowed_roots_for_dir(current_dir, root_real, hub_roots)
        has_config = False
        has_tokenizer = False
        has_weights = False
        config_path: Path | None = None
        tokenizer_config_path: Path | None = None
        dir_size = 0
        gguf_files: list[Path] = []
        for entry in entries:
            entry_path = current_dir / entry.name
            resolved = _resolve_controlled_file(entry_path, allowed_roots)
            if resolved is None:
                continue
            lower = entry.name.lower()
            if lower.endswith(".gguf"):
                if is_polystrata_auxiliary_gguf(resolved):
                    resolved_key = _safe_resolve_text(resolved)
                    if resolved_key not in counted:
                        try:
                            usage += int(resolved.stat().st_size)
                            counted.add(resolved_key)
                        except OSError:
                            pass
                    continue
                gguf_files.append(resolved)
                continue
            if entry.name.startswith("."):
                continue
            try:
                dir_size += resolved.stat().st_size
            except OSError:
                continue
            if lower == "config.json":
                has_config = True
                config_path = resolved
            elif lower in ("tokenizer.json", "tokenizer.model", "tokenizer_config.json"):
                has_tokenizer = True
                if lower == "tokenizer_config.json":
                    tokenizer_config_path = resolved
            elif lower.endswith(".safetensors") or (
                lower.startswith("pytorch_model") and lower.endswith(".bin")
            ):
                has_weights = True
        for info in inspect_gguf_entries(gguf_files):
            asset = _gguf_asset_from_info(info, Path(info["path"]).stem)
            if asset["path"] not in seen_paths:
                seen_paths.add(asset["path"])
                models.append(asset)
                model_sizes[asset["path"]] = asset["size_bytes"] or 0
            if asset["path"] not in counted:
                counted.add(asset["path"])
                usage += model_sizes.get(asset["path"], 0)
        if has_config and has_tokenizer and has_weights:
            dir_key = str(current_dir)
            if dir_key not in seen_paths:
                seen_paths.add(dir_key)
                architecture, context_length = _read_config_summary(config_path)
                chat_ok = _has_chat_template(tokenizer_config_path)
                models.append(
                    {
                        "path": dir_key,
                        "name": current_dir.name,
                        "format": "directory",
                        "size_bytes": dir_size,
                        "architecture": architecture,
                        "context_length": context_length,
                        "available": chat_ok,
                        "reason": None if chat_ok else "chat_template_unavailable",
                    }
                )
                model_sizes[dir_key] = dir_size
            if dir_key not in counted:
                counted.add(dir_key)
                usage += model_sizes.get(dir_key, 0)
    return usage


def _find_receipt(entry_path: Path, target_path: Path, root_real: Path) -> str | None:
    candidates = [entry_path.parent, entry_path.parent.parent]
    if target_path != entry_path:
        candidates.extend([target_path.parent, target_path.parent.parent])
    seen: set[Path] = set()
    for parent in candidates:
        if parent in seen:
            continue
        seen.add(parent)
        receipt = parent / "runtime-receipt.json"
        resolved = _resolve_controlled_file(receipt, (root_real,))
        if resolved is not None:
            return _safe_resolve_text(resolved)
    return None


def _scan_engine_root(
    root: Path, seen_paths: set[str], engines: list[dict], engine_sizes: dict[str, int]
) -> int:
    usage = 0
    counted: set[str] = set()
    for root_real, current_dir, entries in _walk_bounded(root, ENGINE_SCAN_DEPTH):
        for entry in entries:
            if entry.name.lower() not in ENGINE_BINARY_NAMES:
                continue
            entry_path = current_dir / entry.name
            resolved = _resolve_controlled_file(entry_path, (root_real,))
            if resolved is None:
                continue
            try:
                file_stat = resolved.stat()
            except OSError:
                continue
            if not stat.S_ISREG(file_stat.st_mode):
                continue
            if os.name != "nt" and (file_stat.st_mode & 0o111) == 0:
                continue
            binary_path = _safe_resolve_text(resolved)
            if binary_path not in seen_paths:
                seen_paths.add(binary_path)
                engine_sizes[binary_path] = int(file_stat.st_size)
                engines.append(
                    {
                        "path": binary_path,
                        "receipt": _find_receipt(entry_path, resolved, root_real),
                        "modified_ns": int(file_stat.st_mtime_ns),
                    }
                )
            if binary_path not in counted:
                counted.add(binary_path)
                usage += engine_sizes.get(binary_path, 0)
    return usage


def scan_assets(models_roots, engine_roots=()):
    """Scan model and engine roots read-only and return an asset-scan.v1 document."""
    hub_roots = _hf_hub_cache_roots()
    models: list[dict] = []
    seen_models: set[str] = set()
    model_sizes: dict[str, int] = {}
    roots: list[dict] = []
    for root_path, root_text in _unique_roots(models_roots):
        usage = _scan_models_root(root_path, hub_roots, seen_models, models, model_sizes)
        roots.append(
            {
                "path": root_text,
                "kind": "models",
                "disk_usage_bytes": usage,
                "disk_free_bytes": _disk_free_bytes(root_path),
            }
        )
    engines: list[dict] = []
    seen_engines: set[str] = set()
    engine_sizes: dict[str, int] = {}
    for root_path, root_text in _unique_roots(engine_roots):
        usage = _scan_engine_root(root_path, seen_engines, engines, engine_sizes)
        roots.append(
            {
                "path": root_text,
                "kind": "engines",
                "disk_usage_bytes": usage,
                "disk_free_bytes": _disk_free_bytes(root_path),
            }
        )
    models.sort(key=lambda item: item["path"])
    engines.sort(key=lambda item: (-item["modified_ns"], item["path"]))
    total_model_bytes = sum((item["size_bytes"] or 0) for item in models)
    return {
        "definition": "asset-scan.v1",
        "models": models,
        "engines": engines,
        "roots": roots,
        "total_model_bytes": total_model_bytes,
        "model_requests_sent": 0,
    }
