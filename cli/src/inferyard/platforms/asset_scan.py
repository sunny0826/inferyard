"""Read-only discovery of local GGUF models, directory models, and engine binaries."""

from __future__ import annotations

import errno
import json
import os
import stat
from collections.abc import Iterator
from pathlib import Path

from inferyard.platforms.gguf_metadata import read_metadata

MODEL_SCAN_DEPTH = 4
ENGINE_SCAN_DEPTH = 4
SKIP_DIRECTORIES = frozenset({"venv", "env", "node_modules", "site-packages", "__pycache__"})
ENGINE_BINARY_NAMES = frozenset({"llama-server", "llama-server.exe"})


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


def _gguf_asset(path: Path, stem: str) -> dict:
    info = inspect_gguf_file(path, fallback_name=stem)
    if info["status"] == "available":
        chat_ok = bool(info["chat_template_available"])
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
        "name": stem,
        "format": "gguf",
        "size_bytes": None,
        "architecture": None,
        "context_length": None,
        "available": False,
        "reason": "invalid_or_unreadable_gguf",
    }


def _scan_models_root(
    root: Path, hub_roots: tuple[Path, ...], seen_paths: set[str], models: list[dict]
) -> None:
    for root_real, current_dir, entries in _walk_bounded(root, MODEL_SCAN_DEPTH):
        allowed_roots = _allowed_roots_for_dir(current_dir, root_real, hub_roots)
        has_config = False
        has_tokenizer = False
        has_weights = False
        config_path: Path | None = None
        tokenizer_config_path: Path | None = None
        dir_size = 0
        for entry in entries:
            entry_path = current_dir / entry.name
            resolved = _resolve_controlled_file(entry_path, allowed_roots)
            if resolved is None:
                continue
            lower = entry.name.lower()
            if lower.endswith(".gguf"):
                asset = _gguf_asset(resolved, entry_path.stem)
                if asset["path"] not in seen_paths:
                    seen_paths.add(asset["path"])
                    models.append(asset)
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


def _scan_engine_root(root: Path, seen_paths: set[str], engines: list[dict]) -> None:
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
            if binary_path in seen_paths:
                continue
            seen_paths.add(binary_path)
            engines.append(
                {
                    "path": binary_path,
                    "receipt": _find_receipt(entry_path, resolved, root_real),
                    "modified_ns": int(file_stat.st_mtime_ns),
                }
            )


def scan_assets(models_roots, engine_roots=()):
    """Scan model and engine roots read-only and return an asset-scan.v1 document."""
    hub_roots = _hf_hub_cache_roots()
    models: list[dict] = []
    seen_models: set[str] = set()
    for raw_root in models_roots or ():
        if raw_root:
            _scan_models_root(Path(raw_root), hub_roots, seen_models, models)
    engines: list[dict] = []
    seen_engines: set[str] = set()
    for raw_root in engine_roots or ():
        if raw_root:
            _scan_engine_root(Path(raw_root), seen_engines, engines)
    models.sort(key=lambda item: item["path"])
    engines.sort(key=lambda item: (-item["modified_ns"], item["path"]))
    return {
        "definition": "asset-scan.v1",
        "models": models,
        "engines": engines,
        "model_requests_sent": 0,
    }
