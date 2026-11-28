"""Discover and stage existing local Bonsai 2.5 27B and PrismML llama.cpp assets via CoW."""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

from polystrata_stage import clone_tree_cow, snapshot_dir_state, verify_dir_unchanged

MIN_PRISM_BUILD = 12232
_PRISM_TAG_RE = re.compile(r"\bprism-b(\d+)-[0-9a-f]{7,40}\b", re.IGNORECASE)
_PRISM_BANNER_RE = re.compile(r"\(\s*build\s+(\d+)\s*,\s*commit\s+[0-9a-f]{7,40}\s*\)", re.IGNORECASE)


@dataclass(frozen=True)
class BonsaiModelSource:
    model_dir: Path
    gguf_path: Path
    filename: str
    size_bytes: int
    is_loose_file: bool


@dataclass(frozen=True)
class StagedBonsai:
    engine_dir: Path
    engine_bin: Path
    engine_prefix: str
    chosen_model_filename: str
    all_model_filenames: tuple[str, ...]
    src_engine_dir: Path
    src_engine_snapshot: dict[str, str]
    src_model_snapshots: tuple[tuple[Path, dict[str, str]], ...]
    src_loose_file_snapshots: tuple[tuple[Path, int, int], ...]
    host_inferyard: Path | None
    host_inferyard_snapshot: dict[str, str] | None


def _llama_binary_name() -> str:
    return "llama-server.exe" if sys.platform == "win32" else "llama-server"


def is_bonsai_gguf_filename(name: str) -> bool:
    lower = name.lower()
    return (
        lower.endswith(".gguf")
        and "bonsai" in lower
        and ("ptq1_0" in lower or "pq2_0" in lower)
    )


def _studio_settings_path(user_home: Path) -> Path:
    if sys.platform == "darwin":
        return user_home / "Library/Application Support/InferYard Studio/settings.json"
    if sys.platform == "win32":
        appdata = os.environ.get("APPDATA", "").strip()
        base = Path(appdata) if appdata else user_home / "AppData/Roaming"
        return base / "InferYard Studio/settings.json"
    xdg = os.environ.get("XDG_CONFIG_HOME", "").strip()
    base = Path(xdg) if xdg else user_home / ".config"
    return base / "InferYard Studio/settings.json"


def _search_roots(user_home: Path) -> tuple[list[Path], list[Path]]:
    engine_roots: list[Path] = []
    model_roots: list[Path] = []
    seen_eng: set[str] = set()
    seen_mod: set[str] = set()

    def add_eng(candidate: Path) -> None:
        key = str(candidate)
        if key not in seen_eng:
            seen_eng.add(key)
            engine_roots.append(candidate)

    def add_mod(candidate: Path) -> None:
        key = str(candidate)
        if key not in seen_mod:
            seen_mod.add(key)
            model_roots.append(candidate)

    settings_path = _studio_settings_path(user_home)
    if settings_path.is_file():
        try:
            doc = json.loads(settings_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            doc = {}
        if isinstance(doc, dict):
            ws = str(doc.get("workspace_root") or "").strip()
            if ws:
                add_eng(Path(ws).expanduser() / "engines")
                add_mod(Path(ws).expanduser() / "models")
            mr = str(doc.get("models_root") or "").strip()
            if mr:
                add_mod(Path(mr).expanduser())
            extra = doc.get("extra_models_roots")
            if isinstance(extra, list):
                for item in extra:
                    if isinstance(item, str) and item.strip():
                        add_mod(Path(item.strip()).expanduser())

    add_eng(user_home / ".inferyard/engines")
    add_mod(user_home / ".inferyard/models")
    return engine_roots, model_roots


def _prism_build_from_receipts(*dirs: Path) -> int | None:
    seen: set[Path] = set()
    for directory in dirs:
        if directory in seen:
            continue
        seen.add(directory)
        for name in (".inferyard-managed-engine.json", "engine-prepare.json"):
            receipt = directory / name
            if not receipt.is_file():
                continue
            try:
                doc = json.loads(receipt.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError):
                continue
            if not isinstance(doc, dict):
                continue
            for key in ("version", "source_url"):
                val = doc.get(key)
                if isinstance(val, str):
                    match = _PRISM_TAG_RE.search(val)
                    if match is not None:
                        return int(match.group(1))
    return None


def is_prism_llama_build_at_least(bin_path: Path, min_build: int = MIN_PRISM_BUILD) -> bool:
    if not bin_path.is_file() or bin_path.is_symlink():
        return False
    receipt_build = _prism_build_from_receipts(bin_path.parent, bin_path.parent.parent)
    if receipt_build is not None:
        return receipt_build >= min_build
    try:
        proc = subprocess.run(
            [str(bin_path), "--version"],
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return False
    text = f"{proc.stdout}\n{proc.stderr}"
    for pattern in (_PRISM_BANNER_RE, _PRISM_TAG_RE):
        match = pattern.search(text)
        if match is not None:
            return int(match.group(1)) >= min_build
    return False


def _engine_root_dir(bin_path: Path) -> Path:
    parent = bin_path.parent.parent
    for name in (".inferyard-managed-engine.json", "engine-prepare.json"):
        if (parent / name).is_file():
            return parent
    return bin_path.parent


def _resolve_engine_prefix(engine_dir: Path, engine_bin: Path) -> str:
    for root in (engine_bin.parent, engine_bin.parent.parent):
        prep = root / "engine-prepare.json"
        if prep.is_file():
            try:
                doc = json.loads(prep.read_text(encoding="utf-8"))
                out_raw = str(doc.get("out") or "").strip()
                if out_raw:
                    return f"{Path(out_raw).name} · "
            except (OSError, json.JSONDecodeError):
                pass
    if engine_dir.name.lower().startswith("llama-cpp-prism-"):
        return f"{engine_dir.name} · "
    return f"{engine_bin.parent.name} · "


def resolve_local_bonsai_engine(
    override: Path | None = None, *, home: Path | None = None
) -> tuple[Path, Path]:
    want_bin = _llama_binary_name().lower()
    raw_override = override
    if raw_override is None:
        env_raw = os.environ.get("INFERYARD_BONSAI_ENGINE", "").strip()
        if env_raw:
            raw_override = Path(env_raw)
    if raw_override is not None:
        resolved = raw_override.expanduser().resolve()
        if resolved.is_file():
            if not is_prism_llama_build_at_least(resolved):
                raise FileNotFoundError(
                    f"INFERYARD_BONSAI_ENGINE is not PrismML llama.cpp >= b{MIN_PRISM_BUILD}: {resolved}"
                )
            return _engine_root_dir(resolved), resolved
        if resolved.is_dir():
            for cand in sorted(resolved.rglob("*")):
                if cand.is_file() and cand.name.lower() == want_bin and is_prism_llama_build_at_least(cand):
                    return resolved, cand
        raise FileNotFoundError(
            f"No PrismML llama.cpp >= b{MIN_PRISM_BUILD} binary found at {resolved}"
        )

    user_home = home if home is not None else Path.home()
    engine_roots, _ = _search_roots(user_home)
    hits: list[Path] = []
    for root in engine_roots:
        if not root.is_dir():
            continue
        for cand in sorted(root.rglob("*")):
            try:
                rel = cand.relative_to(root)
            except ValueError:
                continue
            if len(rel.parts) > 4:
                continue
            if cand.is_file() and not cand.is_symlink() and cand.name.lower() == want_bin:
                if is_prism_llama_build_at_least(cand):
                    hits.append(cand)
    if not hits:
        raise FileNotFoundError(
            f"No local PrismML llama.cpp >= b{MIN_PRISM_BUILD} found in {[str(r) for r in engine_roots]} "
            "(set --engine or INFERYARD_BONSAI_ENGINE; downloading engines is forbidden)"
        )
    chosen = hits[-1]
    return _engine_root_dir(chosen), chosen


def resolve_local_bonsai_models(
    override: Path | None = None, *, home: Path | None = None
) -> list[BonsaiModelSource]:
    raw_override = override
    if raw_override is None:
        env_raw = os.environ.get("INFERYARD_BONSAI_MODEL", "").strip()
        if env_raw:
            raw_override = Path(env_raw)
    if raw_override is not None:
        resolved = raw_override.expanduser().resolve()
        if resolved.is_file():
            return [
                BonsaiModelSource(
                    model_dir=resolved.parent,
                    gguf_path=resolved,
                    filename=resolved.name,
                    size_bytes=resolved.stat().st_size,
                    is_loose_file=True,
                )
            ]
        if not resolved.is_dir():
            raise FileNotFoundError(f"Local Bonsai model is missing: {resolved}")
        scan_roots = [resolved]
    else:
        user_home = home if home is not None else Path.home()
        _, scan_roots = _search_roots(user_home)

    seen: set[Path] = set()
    found: list[BonsaiModelSource] = []
    for root in scan_roots:
        if not root.is_dir():
            continue
        for cand in sorted(root.rglob("*.gguf")):
            try:
                rel = cand.relative_to(root)
            except ValueError:
                continue
            if len(rel.parts) > 3:
                continue
            if not cand.is_file() or cand.is_symlink() or not is_bonsai_gguf_filename(cand.name):
                continue
            canon = cand.resolve()
            if canon in seen:
                continue
            seen.add(canon)
            found.append(
                BonsaiModelSource(
                    model_dir=cand.parent,
                    gguf_path=cand,
                    filename=cand.name,
                    size_bytes=cand.stat().st_size,
                    is_loose_file=(cand.parent.resolve() == root.resolve()),
                )
            )
    if not found:
        raise FileNotFoundError(
            f"No local Bonsai 2.5 GGUF (PTQ1_0 / PQ2_0) found in {[str(r) for r in scan_roots]} "
            "(set --model or INFERYARD_BONSAI_MODEL; downloading models is forbidden)"
        )
    found.sort(key=lambda item: (item.size_bytes, str(item.gguf_path)))
    return found


def stage_local_bonsai(
    workspace: Path,
    *,
    engine_override: Path | None = None,
    model_override: Path | None = None,
    home: Path | None = None,
) -> StagedBonsai:
    user_home = home if home is not None else Path.home()
    host_inferyard = user_home / ".inferyard"
    host_snap = snapshot_dir_state(host_inferyard) if host_inferyard.is_dir() else None

    src_engine_dir, src_engine_bin = resolve_local_bonsai_engine(engine_override, home=user_home)
    models = resolve_local_bonsai_models(model_override, home=user_home)

    src_engine_snap = snapshot_dir_state(src_engine_dir)
    model_snaps: list[tuple[Path, dict[str, str]]] = []
    seen_dirs: set[Path] = set()
    loose_snaps: list[tuple[Path, int, int]] = []
    for item in models:
        if item.is_loose_file:
            st = item.gguf_path.stat()
            loose_snaps.append((item.gguf_path, st.st_size, st.st_mtime_ns))
        elif item.model_dir not in seen_dirs:
            seen_dirs.add(item.model_dir)
            model_snaps.append((item.model_dir, snapshot_dir_state(item.model_dir)))

    eng_name = src_engine_dir.name
    if not eng_name.lower().startswith("llama-cpp-prism-"):
        eng_name = f"llama-cpp-prism-{eng_name}"
    dst_engine_dir = workspace / "engines" / eng_name
    clone_tree_cow(src_engine_dir, dst_engine_dir)
    rel_bin = src_engine_bin.relative_to(src_engine_dir)
    dst_engine_bin = dst_engine_dir / rel_bin
    engine_prefix = _resolve_engine_prefix(dst_engine_dir, dst_engine_bin)

    cloned_dirs: dict[Path, Path] = {}
    all_filenames: list[str] = []
    for idx, item in enumerate(models):
        if item.is_loose_file:
            dst_dir = workspace / "models" / f"bonsai-staged-{idx}"
            clone_tree_cow(item.gguf_path, dst_dir / item.filename)
            src_json = item.model_dir / "source.json"
            if src_json.is_file():
                clone_tree_cow(src_json, dst_dir / "source.json")
        else:
            if item.model_dir not in cloned_dirs:
                dst_dir = workspace / "models" / item.model_dir.name
                clone_tree_cow(item.model_dir, dst_dir)
                cloned_dirs[item.model_dir] = dst_dir
        all_filenames.append(item.filename)

    return StagedBonsai(
        engine_dir=dst_engine_dir,
        engine_bin=dst_engine_bin,
        engine_prefix=engine_prefix,
        chosen_model_filename=all_filenames[0],
        all_model_filenames=tuple(all_filenames),
        src_engine_dir=src_engine_dir,
        src_engine_snapshot=src_engine_snap,
        src_model_snapshots=tuple(model_snaps),
        src_loose_file_snapshots=tuple(loose_snaps),
        host_inferyard=host_inferyard if host_snap is not None else None,
        host_inferyard_snapshot=host_snap,
    )


def verify_host_bonsai_unchanged(staged: StagedBonsai) -> None:
    verify_dir_unchanged(
        "host Bonsai engine directory",
        staged.src_engine_dir,
        staged.src_engine_snapshot,
    )
    for model_dir, snap in staged.src_model_snapshots:
        verify_dir_unchanged("host Bonsai model directory", model_dir, snap)
    for file_path, want_size, want_mtime in staged.src_loose_file_snapshots:
        if not file_path.is_file():
            raise AssertionError(f"host Bonsai model missing at {file_path}")
        st = file_path.stat()
        if st.st_size != want_size or st.st_mtime_ns != want_mtime:
            raise AssertionError(f"host Bonsai model mutated at {file_path}")
    if staged.host_inferyard is not None and staged.host_inferyard_snapshot is not None:
        verify_dir_unchanged(
            "host ~/.inferyard",
            staged.host_inferyard,
            staged.host_inferyard_snapshot,
        )
