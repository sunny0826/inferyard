"""Catalog, disk-budget, GGUF, sibling-migration, and env checks for PolyStrata preflight."""

from __future__ import annotations

import os
from pathlib import Path
import shutil

from inferyard.contracts.validation import strict_json_loads
from inferyard.platforms.polystrata_sizing import (
    DEFAULT_RUNTIME_DISK_BYTES,
    GGUF_WORK_DISK_BYTES,
    GIB,
    MIB,
    RESUME_RUNTIME_DISK_BYTES,
    load_starter_catalog,
    polystrata_fallback_model_disk_bytes,
    polystrata_min_model_disk_bytes,
    polystrata_model_disk_bytes,
)
from inferyard.provisioning.preparation_io import linked

_DEFAULT_RUNTIME_DISK_BYTES = DEFAULT_RUNTIME_DISK_BYTES
_RESUME_RUNTIME_DISK_BYTES = RESUME_RUNTIME_DISK_BYTES
_GGUF_WORK_DISK_BYTES = GGUF_WORK_DISK_BYTES
_URL_ENV_VARS = (
    "STRATA_ROCM_INDEX",
    "STRATA_PREBUILT_URL",
    "HF_ENDPOINT",
    "STRATA_MODELSCOPE_ENDPOINT",
)
_DATA_ITEMS = ("models", "packs", "mtp")
_load_starter_catalog = load_starter_catalog


def _matching_catalog_models(
    family: str | None,
    model: str | None,
    *,
    system: str | None = None,
    backend: str | None = None,
) -> list[dict]:
    if not family and not model:
        return []
    models = _load_starter_catalog().get("polystrata_models") or []
    fam_norm = family.strip().lower() if isinstance(family, str) and family.strip() else None
    size_norm = model.strip().lower() if isinstance(model, str) and model.strip() else None
    matches: list[dict] = []
    for entry in models:
        entry_fam = str(entry.get("family", "")).lower()
        entry_size = str(entry.get("size", "")).lower()
        if fam_norm and size_norm:
            if entry_fam == fam_norm and entry_size == size_norm:
                matches.append(entry)
        elif fam_norm and entry_fam == fam_norm:
            matches.append(entry)
        elif size_norm and entry_size == size_norm:
            matches.append(entry)
    if matches and system:
        sys_matches = [e for e in matches if system in (e.get("platforms") or {})]
        if sys_matches:
            matches = sys_matches
            if backend in ("cuda", "hip", "metal"):
                backend_matches = [
                    e for e in matches if backend in ((e.get("platforms") or {}).get(system) or [])
                ]
                if backend_matches:
                    matches = backend_matches
    return matches


def _entry_disk_bytes(
    entry: dict, *, system: str, gguf_path: Path | None, avx512: bool | None = None
) -> int:
    return polystrata_model_disk_bytes(entry, system=system, gguf_path=gguf_path, avx512=avx512)


def _existing_ancestor(path: Path) -> Path | None:
    current = Path(path).absolute()
    while True:
        if current.exists():
            return current
        parent = current.parent
        if parent == current:
            return None
        current = parent


def _disk_free_bytes(path: Path, disk_usage_fn=shutil.disk_usage) -> tuple[int | None, int | None]:
    ancestor = _existing_ancestor(path)
    if ancestor is None:
        return None, None
    try:
        free = int(disk_usage_fn(ancestor).free)
    except OSError:
        return None, None
    try:
        dev = ancestor.stat().st_dev
    except OSError:
        dev = None
    return free, dev


_FAMILY_TAG_PREFIX = {
    "qwen36-strata": "qwen36s",
    "kolibri": "kolibri1",
    "deepseek": "dsv4",
}


def _existing_resume_model_bytes(
    data_dir: Path,
    family: str | None,
    model: str | None,
    entry: dict | None,
) -> int:
    """Sum already-downloaded .part/.gguf bytes matching the target model only."""
    if not family or not model:
        return 0
    models_root = data_dir / "models"
    if not models_root.is_dir() or linked(models_root):
        return 0
    gguf_sources = (entry.get("gguf_sources") or {}) if entry is not None else {}
    expected_files = {
        Path(str(raw)).name.lower()
        for raw in (gguf_sources.get("filename"), gguf_sources.get("draft_filename"))
        if raw
    }
    fam_token = family.strip().lower()
    size_token = model.strip().lower()
    tag_prefix = _FAMILY_TAG_PREFIX.get(fam_token, fam_token)
    expected_tag = size_token if fam_token == "qwen" else f"{tag_prefix}-{size_token}"
    total = 0
    stack = [models_root]
    while stack:
        current = stack.pop()
        try:
            with os.scandir(current) as entries:
                for item in entries:
                    if item.is_symlink():
                        continue
                    if item.is_dir(follow_symlinks=False):
                        stack.append(Path(item.path))
                    elif item.is_file(follow_symlinks=False):
                        lower = item.name.lower()
                        if not lower.endswith((".part", ".gguf")):
                            continue
                        base_name = lower[:-5] if lower.endswith(".part") else lower
                        rel_parts = current.relative_to(models_root).parts
                        top_dir = rel_parts[0].lower() if rel_parts else ""
                        if expected_files:
                            matched = base_name in expected_files
                        else:
                            matched = (
                                top_dir == expected_tag
                                and size_token in base_name
                                and (fam_token != "qwen" or not base_name.startswith("swift-"))
                            )
                        if matched:
                            try:
                                total += item.stat(follow_symlinks=False).st_size
                            except OSError:
                                pass
        except OSError:
            continue
    return total


def _estimate_model_disk_bytes(
    *,
    system: str,
    backend: str | None = None,
    family: str | None,
    model: str | None,
    gguf_path: Path | None,
    resume: bool,
    data_dir: Path,
    avx512: bool | None = None,
) -> tuple[int, bool]:
    matches = _matching_catalog_models(family, model, system=system, backend=backend)
    if not matches:
        if gguf_path is not None:
            return GGUF_WORK_DISK_BYTES, True
        if not family and not model:
            return (
                polystrata_fallback_model_disk_bytes(
                    system=system, backend=backend, avx512=avx512
                ),
                False,
            )
        return polystrata_min_model_disk_bytes(system=system, backend=backend), False
    best_entry = min(
        matches,
        key=lambda e: _entry_disk_bytes(e, system=system, gguf_path=gguf_path, avx512=avx512),
    )
    base = _entry_disk_bytes(best_entry, system=system, gguf_path=gguf_path, avx512=avx512)
    exact = gguf_path is not None or bool(family and model)
    if gguf_path is None and (resume or (data_dir.is_dir() and not linked(data_dir))):
        download_cap = int(best_entry.get("download_bytes") or 0)
        already = min(
            download_cap, _existing_resume_model_bytes(data_dir, family, model, best_entry)
        )
        if already > 0 or resume:
            return max(GGUF_WORK_DISK_BYTES, base - already), exact
    return base, exact


def _required_model_disk_bytes(
    *,
    system: str,
    family: str | None,
    model: str | None,
    gguf_path: Path | None,
    resume: bool,
    data_dir: Path,
    backend: str | None = None,
    avx512: bool | None = None,
) -> int:
    required, _exact = _estimate_model_disk_bytes(
        system=system,
        backend=backend,
        family=family,
        model=model,
        gguf_path=gguf_path,
        resume=resume,
        data_dir=data_dir,
        avx512=avx512,
    )
    return required


def _resolve_macos_gguf_choice(
    gguf_path: Path,
    family: str | None,
    model: str | None,
) -> tuple[str | None, str | None, list[dict]]:
    """Resolve explicit family/model for macOS --gguf-path so setup_mac.py never picks another model."""
    gguf_files = (
        [gguf_path]
        if gguf_path.is_file()
        else sorted(p for p in gguf_path.iterdir() if p.is_file() and p.suffix.lower() == ".gguf")
    )
    file_names = {p.name.lower() for p in gguf_files}
    catalog_models = _load_starter_catalog().get("polystrata_models") or []

    if family and model:
        matches = _matching_catalog_models(family, model)
        if matches:
            entry = matches[0]
            if "Darwin" not in (entry.get("platforms") or {}):
                return family, model, [
                    {
                        "code": "polystrata_gguf_unmatched_model",
                        "message": f"Model {family}/{model} is not supported on macOS Metal.",
                        "fix_command": None,
                    }
                ]
            expected_name = (entry.get("gguf_sources") or {}).get("filename")
            if expected_name and expected_name.lower() not in file_names:
                return family, model, [
                    {
                        "code": "polystrata_gguf_unmatched_model",
                        "message": (
                            f"Expected GGUF '{expected_name}' for {family}/{model} was not found "
                            "at --gguf-path."
                        ),
                        "fix_command": None,
                    }
                ]
            if not expected_name and gguf_path.is_file():
                other_known = {
                    str((item.get("gguf_sources") or {}).get("filename") or "").lower()
                    for item in catalog_models
                    if (item.get("gguf_sources") or {}).get("filename")
                }
                fname_lower = gguf_path.name.lower()
                if fname_lower in other_known or model.strip().lower() not in fname_lower:
                    return family, model, [
                        {
                            "code": "polystrata_gguf_unmatched_model",
                            "message": (
                                f"File '{gguf_path.name}' does not match {family}/{model} "
                                "expected shards."
                            ),
                            "fix_command": None,
                        }
                    ]
        return family, model, []

    inferred: list[dict] = []
    for entry in catalog_models:
        if "Darwin" not in (entry.get("platforms") or {}):
            continue
        expected_name = (entry.get("gguf_sources") or {}).get("filename")
        if expected_name and expected_name.lower() in file_names:
            if family and str(entry.get("family", "")).lower() != family.lower():
                continue
            if model and str(entry.get("size", "")).lower() != model.lower():
                continue
            inferred.append(entry)
    if len(inferred) == 1:
        return str(inferred[0]["family"]), str(inferred[0]["size"]), []
    return family, model, [
        {
            "code": "polystrata_gguf_unmatched_model",
            "message": (
                "Could not uniquely match --gguf-path to a macOS PolyStrata model; "
                "specify --family and --model matching the GGUF file."
            ),
            "fix_command": None,
        }
    ]


def _validate_gguf_path(gguf_path: Path) -> list[dict]:
    if not gguf_path.exists():
        return [
            {
                "code": "polystrata_gguf_path_missing",
                "message": "The provided --gguf-path does not exist.",
                "fix_command": None,
            }
        ]
    if gguf_path.is_file():
        if gguf_path.suffix.lower() != ".gguf":
            return [
                {
                    "code": "polystrata_gguf_path_invalid",
                    "message": "--gguf-path file must end with .gguf.",
                    "fix_command": None,
                }
            ]
        return []
    if gguf_path.is_dir():
        has_gguf = any(
            entry.is_file() and entry.name.lower().endswith(".gguf")
            for entry in gguf_path.iterdir()
        )
        if not has_gguf:
            return [
                {
                    "code": "polystrata_gguf_path_invalid",
                    "message": "--gguf-path directory does not contain any .gguf files.",
                    "fix_command": None,
                }
            ]
        return []
    return [
        {
            "code": "polystrata_gguf_path_invalid",
            "message": "--gguf-path must be a .gguf file or directory of .gguf shards.",
            "fix_command": None,
        }
    ]


def _has_polystrata_data(folder: Path) -> bool:
    for item in _DATA_ITEMS:
        sub = folder / item
        try:
            if sub.is_dir() and not linked(sub) and any(sub.iterdir()):
                return True
        except OSError:
            continue
    return False


def _check_resume_saved_data_dir(out: Path, data_dir: Path) -> list[dict]:
    """Block --resume if isolated PolyStrata settings.json points to a different existing data_dir."""
    dest_norm = data_dir.resolve(strict=False)
    for rel in (
        Path(".inferyard-config") / "polystrata" / "settings.json",
        Path(".inferyard-config") / "PolyStrata" / "settings.json",
    ):
        settings_file = out / rel
        if not settings_file.is_file() or linked(settings_file):
            continue
        try:
            saved = strict_json_loads(settings_file.read_text(encoding="utf-8"))
        except (OSError, ValueError, UnicodeError):
            continue
        prev_raw = saved.get("data_dir") if isinstance(saved, dict) else None
        if isinstance(prev_raw, str) and prev_raw.strip():
            prev_path = Path(prev_raw).expanduser().resolve(strict=False)
            if prev_path != dest_norm:
                return [
                    {
                        "code": "polystrata_data_dir_mismatch",
                        "message": (
                            "Cannot change --data-dir when resuming an existing PolyStrata "
                            "installation; reuse the original --data-dir."
                        ),
                        "fix_command": None,
                    }
                ]
    return []


def _detect_sibling_migration_risk(out: Path, data_dir: Path) -> list[dict]:
    """Mirror upstream other_installs() scan and block before setup.py can move sibling data."""
    saved_blockers = _check_resume_saved_data_dir(out, data_dir)
    if saved_blockers:
        return saved_blockers
    dest_ancestor = _existing_ancestor(data_dir)
    if dest_ancestor is None:
        return []
    try:
        dest_dev = dest_ancestor.stat().st_dev
    except OSError:
        return []
    out_norm = out.resolve(strict=False)
    dest_norm = data_dir.resolve(strict=False)
    candidates: list[Path] = []
    for base in dict.fromkeys((out.parent, out.parent.parent)):
        if not base.is_dir():
            continue
        try:
            for child in base.iterdir():
                if child.is_dir() and child.name.lower().startswith("polystrata"):
                    candidates.append(child)
                    try:
                        for sub in child.iterdir():
                            if sub.is_dir() and sub.name.lower().startswith("polystrata"):
                                candidates.append(sub)
                    except OSError:
                        pass
        except OSError:
            continue
    seen: set[Path] = set()
    for cand in candidates:
        try:
            resolved = cand.resolve(strict=True)
        except OSError:
            continue
        has_setup = (resolved / "setup.py").is_file() or (
            resolved / "terminal" / "setup.py"
        ).is_file()
        if resolved == out_norm or resolved in seen or not has_setup:
            continue
        seen.add(resolved)
        for check_dir in (resolved, resolved / "PolyStrata-data"):
            if check_dir.resolve(strict=False) == dest_norm:
                continue
            try:
                if (
                    check_dir.is_dir()
                    and check_dir.stat().st_dev == dest_dev
                    and _has_polystrata_data(check_dir)
                ):
                    return [
                        {
                            "code": "polystrata_sibling_migration_risk",
                            "message": (
                                "Another PolyStrata installation with local model data exists in "
                                "an adjacent directory; upstream setup.py would migrate its files. "
                                "Place --out under a dedicated parent directory."
                            ),
                            "fix_command": None,
                        }
                    ]
            except OSError:
                continue
    return []


def _check_credentialed_env_urls(env: dict[str, str]) -> list[dict]:
    for key in _URL_ENV_VARS:
        val = env.get(key)
        if isinstance(val, str) and "://" in val and "@" in val.split("/", 3)[2]:
            return [
                {
                    "code": "polystrata_credentialed_env_url",
                    "message": (
                        f"Environment variable {key} contains embedded URL credentials, "
                        "which upstream setup.py could log or persist to disk."
                    ),
                    "fix_command": None,
                }
            ]
    return []
