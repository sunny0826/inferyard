"""Discover and stage existing local PolyStrata assets into an isolated CoW workspace."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

WEIGHT_FLAGS = frozenset({"--pack", "--native", "--ple-gguf", "--model", "--mtp"})


@dataclass(frozen=True)
class StagedPolystrata:
    installation_path: Path
    work_dir: Path
    data_dir: Path
    model_name: str
    src_work_dir: Path
    src_data_dir: Path
    work_snapshot: dict[str, str]
    data_snapshot: dict[str, str]


def _studio_settings_path(home: Path) -> Path:
    if sys.platform == "darwin":
        return home / "Library/Application Support/InferYard Studio/settings.json"
    if sys.platform == "win32":
        appdata = os.environ.get("APPDATA", "").strip()
        base = Path(appdata) if appdata else home / "AppData/Roaming"
        return base / "InferYard Studio/settings.json"
    xdg = os.environ.get("XDG_CONFIG_HOME", "").strip()
    base = Path(xdg) if xdg else home / ".config"
    return base / "InferYard Studio/settings.json"


def _usable_polystrata_installation(path: Path) -> bool:
    if not path.is_file():
        return False
    try:
        decl = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    if not isinstance(decl, dict) or decl.get("family") != "polystrata":
        return False
    work_dir = Path(str(decl.get("working_directory") or path.parent)).expanduser()
    raw_data = str(decl.get("data_dir") or "").strip()
    return bool(raw_data) and work_dir.is_dir() and Path(raw_data).expanduser().is_dir()


def _scan_polystrata_engine_dirs(engines_root: Path) -> list[Path]:
    if not engines_root.is_dir():
        return []
    hits: list[tuple[int, str, Path]] = []
    try:
        children = list(engines_root.iterdir())
    except OSError:
        return []
    for child in children:
        lower = child.name.lower()
        if not child.is_dir() or child.is_symlink():
            continue
        if lower != "polystrata" and not lower.startswith("polystrata-"):
            continue
        inst = child / "installation.json"
        try:
            st = inst.stat()
        except OSError:
            continue
        if inst.is_file():
            hits.append((st.st_mtime_ns, str(inst), inst))
    hits.sort(reverse=True)
    return [item[2] for item in hits]


def resolve_local_installation(override: Path | None, *, home: Path | None = None) -> Path:
    if override is not None:
        resolved = override.expanduser().resolve()
        if not resolved.is_file():
            raise FileNotFoundError(f"PolyStrata installation.json is missing: {resolved}")
        return resolved
    env_override = os.environ.get("INFERYARD_POLYSTRATA_INSTALLATION", "").strip()
    if env_override:
        resolved = Path(env_override).expanduser().resolve()
        if not resolved.is_file():
            raise FileNotFoundError(
                f"INFERYARD_POLYSTRATA_INSTALLATION is missing: {resolved}"
            )
        return resolved

    user_home = home if home is not None else Path.home()
    workspaces: list[Path] = []
    registered: list[Path] = []
    settings_path = _studio_settings_path(user_home)
    if settings_path.is_file():
        try:
            settings_doc = json.loads(settings_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            settings_doc = {}
        if isinstance(settings_doc, dict):
            ws_raw = str(settings_doc.get("workspace_root") or "").strip()
            if ws_raw:
                workspaces.append(Path(ws_raw).expanduser())
            raw_insts = settings_doc.get("engine_installations")
            if isinstance(raw_insts, list):
                for item in reversed(raw_insts):
                    if isinstance(item, str) and item.strip():
                        registered.append(Path(item.strip()).expanduser())
    workspaces.append(user_home / ".inferyard")

    candidates: list[Path] = []
    seen: set[str] = set()

    def add_candidate(path: Path) -> None:
        key = str(path)
        if key not in seen:
            seen.add(key)
            candidates.append(path)

    for ws in workspaces:
        for inst in _scan_polystrata_engine_dirs(ws / "engines"):
            add_candidate(inst)
        add_candidate(ws / "engines/polystrata/installation.json")
    for inst in registered:
        add_candidate(inst)
    add_candidate(user_home / "PolyStrata/installation.json")
    add_candidate(user_home / "polystrata/installation.json")

    for candidate in candidates:
        if _usable_polystrata_installation(candidate):
            return candidate.resolve()
    searched = [
        str(user_home / ".inferyard/engines/polystrata*/installation.json"),
        str(settings_path),
        str(user_home / "PolyStrata/installation.json"),
        str(user_home / "polystrata/installation.json"),
    ]
    raise FileNotFoundError(
        f"no local PolyStrata installation.json found in {searched} "
        "(downloading models is forbidden; pass --installation or INFERYARD_POLYSTRATA_INSTALLATION)"
    )


def snapshot_dir_state(root: Path) -> dict[str, str]:
    snap: dict[str, str] = {}
    for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
        current = Path(dirpath)
        rel_dir = current.relative_to(root)
        kept_dirs: list[str] = []
        for dirname in sorted(dirnames):
            child = current / dirname
            rel = str(rel_dir / dirname) if rel_dir != Path(".") else dirname
            if child.is_symlink():
                snap[rel] = f"sym:{os.readlink(child)}"
            elif dirname == ".venv":
                snap[rel] = "dir:.venv"
            else:
                snap[rel] = "dir"
                kept_dirs.append(dirname)
        dirnames[:] = kept_dirs
        for filename in sorted(filenames):
            child = current / filename
            rel = str(rel_dir / filename) if rel_dir != Path(".") else filename
            if child.is_symlink():
                snap[rel] = f"sym:{os.readlink(child)}"
            else:
                st = child.lstat()
                snap[rel] = f"file:{st.st_size}:{st.st_mtime_ns}"
    return snap


def verify_dir_unchanged(label: str, root: Path, expected: dict[str, str]) -> None:
    actual = snapshot_dir_state(root)
    if actual != expected:
        raise RuntimeError(f"{label} was mutated during UI E2E: {root}")


def clone_tree_cow(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    if sys.platform == "darwin":
        cp_cow = subprocess.run(
            ["/bin/cp", "-cR", str(src), str(dst)], capture_output=True, check=False
        )
        if cp_cow.returncode == 0:
            return
    subprocess.run(["cp", "-R", str(src), str(dst)], check=True)


def rewrite_prefixes(path: Path, replacements: list[tuple[str, str]]) -> None:
    raw = path.read_text(encoding="utf-8")
    updated = raw
    for old, new in replacements:
        if old and old != new:
            updated = updated.replace(old, new)
    if updated != raw:
        path.write_text(updated, encoding="utf-8")


def estimate_strata_disk_bytes(cfg_path: Path) -> int:
    try:
        doc = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return 1 << 60
    args = doc.get("args")
    if not isinstance(args, list):
        return 1 << 60
    total = 0
    for idx in range(len(args) - 1):
        if args[idx] in WEIGHT_FLAGS and isinstance(args[idx + 1], str):
            target = Path(args[idx + 1])
            if target.is_file():
                total += target.stat().st_size
            elif target.is_dir():
                for root, _, files in os.walk(target):
                    for name in files:
                        fp = Path(root) / name
                        if fp.is_file() and not fp.is_symlink():
                            total += fp.stat().st_size
    return total if total > 0 else (1 << 60)


def _strata_config_usable(cfg_path: Path, dst_work: Path) -> bool:
    try:
        doc = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False
    if not isinstance(doc, dict):
        return False
    exe = str(doc.get("exe") or "").strip()
    if not exe:
        return False
    exe_path = Path(exe)
    if exe_path.is_absolute():
        exe_ok = exe_path.is_file()
    elif "/" in exe or "\\" in exe:
        cwd = Path(str(doc.get("cwd") or dst_work))
        exe_ok = (cwd / exe_path).is_file() or (dst_work / exe_path).is_file()
    else:
        exe_ok = shutil.which(exe) is not None
    return exe_ok and estimate_strata_disk_bytes(cfg_path) < (1 << 60)


def _polystrata_config_search_dirs(dst_work: Path, dst_data: Path) -> list[Path]:
    dirs = [dst_work, dst_work / "terminal", dst_data, dst_data / "models"]
    models_dir = dst_data / "models"
    if models_dir.is_dir() and not models_dir.is_symlink():
        for sub in sorted(models_dir.iterdir()):
            if sub.is_dir() and not sub.is_symlink():
                dirs.append(sub)
    return dirs


def polystrata_config_display_name(cfg_path: Path) -> str:
    try:
        doc = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        doc = {}
    if isinstance(doc, dict):
        for key in ("display_name", "model_name"):
            raw = doc.get(key)
            if isinstance(raw, str):
                cleaned = " ".join(raw.split())
                if cleaned:
                    return cleaned
    return cfg_path.stem or cfg_path.name


def stage_local_polystrata(src_inst_path: Path, workspace: Path) -> StagedPolystrata:
    decl = json.loads(src_inst_path.read_text(encoding="utf-8"))
    if decl.get("family") != "polystrata":
        raise ValueError(f"expected family=polystrata in {src_inst_path}")
    src_work = Path(str(decl.get("working_directory") or src_inst_path.parent)).expanduser()
    raw_data = str(decl.get("data_dir") or "").strip()
    if not raw_data:
        raise ValueError(f"missing data_dir in {src_inst_path}")
    src_data = Path(raw_data).expanduser()

    work_snap = snapshot_dir_state(src_work)
    data_snap = snapshot_dir_state(src_data)

    dst_work = workspace / "engines/polystrata"
    dst_data = workspace / "models/polystrata-data"
    clone_tree_cow(src_work, dst_work)
    clone_tree_cow(src_data, dst_data)

    replacements = [
        (str(src_work), str(dst_work)),
        (str(src_work.resolve()), str(dst_work)),
        (str(src_data), str(dst_data)),
        (str(src_data.resolve()), str(dst_data)),
    ]
    dst_inst = dst_work / "installation.json"
    rewrite_prefixes(dst_inst, replacements)
    for rel_cfg in (
        Path(".inferyard-config/polystrata/settings.json"),
        Path(".inferyard-config/PolyStrata/settings.json"),
    ):
        cfg_file = dst_work / rel_cfg
        if cfg_file.is_file() and not cfg_file.is_symlink():
            rewrite_prefixes(cfg_file, replacements)

    configs: list[tuple[tuple[int, int, int, str], Path]] = []
    seen_files: set[Path] = set()
    for search_dir in _polystrata_config_search_dirs(dst_work, dst_data):
        if not search_dir.is_dir() or search_dir.is_symlink():
            continue
        for entry in sorted(search_dir.iterdir()):
            if not entry.is_file() or entry.is_symlink():
                continue
            resolved_entry = entry.resolve()
            if resolved_entry in seen_files:
                continue
            seen_files.add(resolved_entry)
            if entry.suffix == ".log":
                entry.unlink(missing_ok=True)
                continue
            if entry.name.startswith("strata-") and entry.name.endswith(".json"):
                rewrite_prefixes(entry, replacements)
                usable_rank = 0 if _strata_config_usable(entry, dst_work) else 1
                local_rank = 0 if entry.is_relative_to(dst_work) else 1
                disk_bytes = estimate_strata_disk_bytes(entry)
                configs.append(((usable_rank, local_rank, disk_bytes, str(entry)), entry))
    if not configs:
        raise RuntimeError(f"no strata-*.json found in staged PolyStrata ({dst_work}, {dst_data})")
    configs.sort(key=lambda item: item[0])
    chosen = configs[0][1]
    for _, extra in configs[1:]:
        extra.unlink(missing_ok=True)
    model_name = polystrata_config_display_name(chosen)
    return StagedPolystrata(
        installation_path=dst_inst,
        work_dir=dst_work,
        data_dir=dst_data,
        model_name=model_name,
        src_work_dir=src_work,
        src_data_dir=src_data,
        work_snapshot=work_snap,
        data_snapshot=data_snap,
    )
