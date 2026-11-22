"""Read-only PolyStrata installation detection across macOS, Linux, and Windows."""

from __future__ import annotations

import json
import os
from pathlib import Path

POLYSTRATA_ENGINE_DIRS = ("engine", "engine-poly", "build-metal")
_MAX_BUILD_JSON_BYTES = 64 * 1024


def _safe_resolve(path: Path) -> Path | None:
    try:
        return path.resolve(strict=True)
    except OSError:
        return None


def pick_venv_python(venv_dir: Path, preferred: Path | None = None) -> Path | None:
    """Locate the Python executable inside a POSIX (bin/) or Windows (Scripts/) venv."""
    candidates: list[Path] = []
    bin_dir = venv_dir / "bin"
    scripts_dir = venv_dir / "Scripts"
    if preferred is not None and preferred.parent in (bin_dir, scripts_dir):
        candidates.append(preferred)
    for directory, names in (
        (bin_dir, ("python", "python3")),
        (scripts_dir, ("python.exe", "python")),
    ):
        for name in names:
            candidate = directory / name
            if candidate not in candidates:
                candidates.append(candidate)
    existing = [p for p in candidates if p.is_file()]
    if not existing:
        return None
    for p in existing:
        if os.access(p, os.X_OK):
            return p
    return existing[0]


def polystrata_runtime_roots(root: Path, executable: Path | None = None) -> list[str]:
    """Return runtime directories to freeze (excluding model weight data dirs)."""
    dirs: list[Path] = [root / "serve"]
    if executable is not None and (executable.parent.parent / "pyvenv.cfg").is_file():
        dirs.append(executable.parent.parent)
    elif (root / ".venv" / "pyvenv.cfg").is_file():
        dirs.append(root / ".venv")
    for name in POLYSTRATA_ENGINE_DIRS:
        dirs.append(root / name)
    resolved: list[str] = []
    for item in dirs:
        if item.is_dir() and not item.is_symlink():
            target = _safe_resolve(item)
            if target is not None and str(target) not in resolved:
                resolved.append(str(target))
    if not resolved:
        target = _safe_resolve(root)
        if target is not None:
            resolved.append(str(target))
    return sorted(resolved)


def _read_build_json(root: Path) -> dict | None:
    build_path = root / "BUILD.json"
    try:
        if not build_path.is_file() or build_path.stat().st_size > _MAX_BUILD_JSON_BYTES:
            return None
        data = json.loads(build_path.read_text(encoding="utf-8"))
        return data if isinstance(data, dict) else None
    except (OSError, ValueError, UnicodeError):
        return None


def read_build_version(root: Path) -> str | None:
    data = _read_build_json(root)
    if not data:
        return None
    for key in ("version", "release", "tag", "commit"):
        value = data.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return None


def read_build_backend(root: Path) -> str | None:
    data = _read_build_json(root)
    if not data:
        return None
    value = data.get("backend")
    if isinstance(value, str):
        norm = value.strip().lower()
        if norm in ("metal", "cuda", "hip", "cpu"):
            return norm
        if norm == "rocm":
            return "hip"
    return None


def _default_data_dir(root: Path) -> Path | None:
    for candidate in (root / "data", root.parent / "PolyStrata-data"):
        if candidate.is_dir() and not candidate.is_symlink():
            resolved = _safe_resolve(candidate)
            if resolved is not None:
                return resolved
    return None


def polystrata_model_configs(root: Path, data_dir: Path | None = None) -> list[str]:
    """List strata-*.json files inside a PolyStrata root and optional data directory."""
    search_dirs: list[Path] = [root]
    if data_dir is not None and data_dir.is_dir():
        search_dirs.append(data_dir)
        models_sub = data_dir / "models"
        if models_sub.is_dir():
            search_dirs.append(models_sub)
            try:
                with os.scandir(models_sub) as entries:
                    for entry in entries:
                        if entry.is_dir(follow_symlinks=False):
                            search_dirs.append(Path(entry.path))
            except OSError:
                pass
    found: set[str] = set()
    for directory in search_dirs:
        try:
            with os.scandir(directory) as entries:
                for entry in entries:
                    if (
                        entry.is_file()
                        and entry.name.startswith("strata-")
                        and entry.name.endswith(".json")
                    ):
                        resolved = _safe_resolve(Path(entry.path))
                        if resolved is not None:
                            found.add(str(resolved))
        except OSError:
            continue
    return sorted(found)


def is_polystrata_root(directory: Path) -> bool:
    return (
        directory.is_dir()
        and (directory / "serve" / "server.py").is_file()
        and ((directory / "setup.py").is_file() or (directory / "polystrata.py").is_file())
    )


def infer_polystrata_root(executable: Path) -> Path | None:
    """Infer PolyStrata working_directory from a .venv Python executable."""
    venv_dir = executable.parent.parent
    if executable.parent.name in ("bin", "Scripts") and (venv_dir / "pyvenv.cfg").is_file():
        candidate = venv_dir.parent
        if is_polystrata_root(candidate):
            return _safe_resolve(candidate)
    return None


def probe_polystrata_root(
    root_dir: Path,
    seen: set[tuple[str, str]],
    out: list[dict],
    *,
    preferred_python: Path | None = None,
) -> bool:
    resolved = _safe_resolve(root_dir)
    if resolved is None or not is_polystrata_root(resolved):
        return False
    venv_dir = resolved / ".venv"
    if not (venv_dir / "pyvenv.cfg").is_file():
        return False
    python_exec = pick_venv_python(venv_dir, preferred=preferred_python)
    if python_exec is None:
        return False
    if not any(
        (resolved / name).is_dir() and not (resolved / name).is_symlink()
        for name in POLYSTRATA_ENGINE_DIRS
    ):
        return False
    dedupe_key = ("polystrata", str(resolved))
    if dedupe_key in seen:
        return True
    seen.add(dedupe_key)
    version = read_build_version(resolved)
    data_dir = _default_data_dir(resolved)
    configs = polystrata_model_configs(resolved, data_dir)
    label = (
        f"polystrata · {version} ({resolved})"
        if version
        else f"polystrata · {resolved.name} ({resolved})"
    )
    out.append(
        {
            "family": "polystrata",
            "executable": str(python_exec),
            "working_directory": str(resolved),
            "runtime_roots": polystrata_runtime_roots(resolved, python_exec),
            "models_root": None,
            "data_dir": str(data_dir) if data_dir is not None else None,
            "version": version,
            "model_configs": configs,
            "label": label,
        }
    )
    return True


def _matching_children(parent: Path, prefix: str) -> list[Path]:
    if not parent.is_dir():
        return []
    found: list[Path] = []
    try:
        with os.scandir(parent) as entries:
            for entry in sorted(entries, key=lambda item: item.name):
                if entry.is_dir() and entry.name.lower().startswith(prefix):
                    found.append(Path(entry.path))
    except OSError:
        return []
    return found


def collect_polystrata_targets(
    extra_paths: tuple | list, home: Path, cwd: Path
) -> list[tuple[Path, Path | None]]:
    """Collect candidate PolyStrata root directories from explicit and standard locations."""
    targets: list[tuple[Path, Path | None]] = []
    for raw in extra_paths:
        if not raw:
            continue
        path = Path(raw)
        if path.is_file():
            name = path.name.lower()
            if name == "server.py" and path.parent.name == "serve":
                targets.append((path.parent.parent, None))
            elif name in ("setup.py", "polystrata.py", "build.json"):
                targets.append((path.parent, None))
            elif name.startswith("python"):
                inferred = infer_polystrata_root(path)
                if inferred is not None:
                    targets.append((inferred, path))
        elif path.is_dir():
            if is_polystrata_root(path):
                targets.append((path, None))
            else:
                try:
                    with os.scandir(path) as entries:
                        for entry in sorted(entries, key=lambda item: item.name):
                            if entry.is_dir():
                                child = Path(entry.path)
                                if is_polystrata_root(child):
                                    targets.append((child, None))
                except OSError:
                    pass
    if env_root := os.environ.get("POLYSTRATA_ROOT"):
        targets.append((Path(env_root), None))
    for child in _matching_children(cwd / "engines", "polystrata"):
        targets.append((child, None))
    for child in _matching_children(home / ".inferyard" / "engine", "polystrata"):
        targets.append((child, None))
    targets.extend([(home / "PolyStrata", None), (home / "polystrata", None)])
    return targets
