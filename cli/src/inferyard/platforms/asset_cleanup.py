"""Safe local model and engine asset deletion with containment and evidence guards."""

from __future__ import annotations

import os
import shutil
import stat
from collections.abc import Iterable
from pathlib import Path

SEALED_EVIDENCE_MARKERS = frozenset(
    {"manifest.json", "events.jsonl", "studio.json", "run-result.json"}
)


def _canonical_allowed_roots(allowed_roots: Iterable[str | Path]) -> list[Path]:
    roots: list[Path] = []
    for raw in allowed_roots:
        if not raw or "\x00" in str(raw):
            continue
        text = str(raw)
        if not text.strip():
            continue
        candidate = Path(text)
        if not candidate.is_absolute():
            continue
        try:
            resolved = candidate.resolve(strict=True)
        except OSError:
            continue
        if resolved.is_dir():
            roots.append(resolved)
    return list(dict.fromkeys(roots))


def _canonical_protected_roots(protected_roots: Iterable[str | Path]) -> list[Path]:
    roots: list[Path] = []
    for raw in protected_roots:
        if not raw or "\x00" in str(raw):
            continue
        text = str(raw)
        if not text.strip():
            continue
        candidate = Path(text)
        if not candidate.is_absolute():
            continue
        try:
            resolved = candidate.resolve(strict=False)
        except OSError:
            continue
        roots.append(resolved)
    return list(dict.fromkeys(roots))


def _raise_walk_error(err: OSError) -> None:
    raise err


def _scan_delete_bytes(resolved_target: Path, target_lstat: os.stat_result) -> int:
    if not stat.S_ISDIR(target_lstat.st_mode):
        if resolved_target.name in SEALED_EVIDENCE_MARKERS:
            raise ValueError("asset_delete_protected_evidence")
        return target_lstat.st_size if stat.S_ISREG(target_lstat.st_mode) else 0

    reclaimed = 0
    for dirpath, dirnames, filenames in os.walk(
        resolved_target,
        topdown=True,
        followlinks=False,
        onerror=_raise_walk_error,
    ):
        current = Path(dirpath)
        for dirname in dirnames:
            if (current / dirname).is_symlink():
                raise ValueError("asset_delete_symlink_refused")
        for filename in filenames:
            entry = current / filename
            entry_lstat = entry.lstat()
            if stat.S_ISLNK(entry_lstat.st_mode):
                raise ValueError("asset_delete_symlink_refused")
            if filename in SEALED_EVIDENCE_MARKERS:
                raise ValueError("asset_delete_protected_evidence")
            if stat.S_ISREG(entry_lstat.st_mode):
                reclaimed += entry_lstat.st_size
    return reclaimed


def delete_asset_safely(
    target_path: str | Path,
    *,
    allowed_roots: Iterable[str | Path],
    protected_roots: Iterable[str | Path] = (),
) -> dict[str, str | int]:
    """Delete a local asset inside allowed_roots after verifying symlink and evidence safety."""
    if not isinstance(target_path, (str, Path)):
        raise ValueError("asset_delete_not_found")
    raw_text = str(target_path)
    if not raw_text.strip() or "\x00" in raw_text:
        raise ValueError("asset_delete_not_found")
    target = Path(raw_text)
    if not target.is_absolute():
        raise ValueError("asset_delete_outside_allowed_roots")

    try:
        target_lstat = target.lstat()
    except OSError as exc:
        raise ValueError("asset_delete_not_found") from exc

    if stat.S_ISLNK(target_lstat.st_mode):
        raise ValueError("asset_delete_symlink_refused")

    try:
        resolved_target = target.resolve(strict=True)
    except OSError as exc:
        raise ValueError("asset_delete_not_found") from exc

    canonical_allowed = _canonical_allowed_roots(allowed_roots)
    if not any(
        resolved_target != root and resolved_target.is_relative_to(root)
        for root in canonical_allowed
    ):
        raise ValueError("asset_delete_outside_allowed_roots")

    for prot in _canonical_protected_roots(protected_roots):
        if (
            resolved_target == prot
            or resolved_target.is_relative_to(prot)
            or prot.is_relative_to(resolved_target)
        ):
            raise ValueError("asset_delete_protected_evidence")

    reclaimed = _scan_delete_bytes(resolved_target, target_lstat)
    if stat.S_ISDIR(target_lstat.st_mode):
        shutil.rmtree(resolved_target)
    else:
        resolved_target.unlink()
    return {"deleted": str(resolved_target), "reclaimed_bytes": reclaimed}
