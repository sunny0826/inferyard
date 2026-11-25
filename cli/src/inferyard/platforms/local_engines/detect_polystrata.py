"""Read-only PolyStrata installation detection and layout verification across platforms."""

from __future__ import annotations

import json
import os
import platform
from pathlib import Path

from inferyard.errors import PreflightError

POLYSTRATA_ENGINE_DIRS = ("engine", "engine-poly", "build-metal")
POLYSTRATA_BUILD_JSON_NAMES = ("BUILD.json", "BUILD-poly.json", "BUILD-q36.json")
_MAX_BUILD_JSON_BYTES = 64 * 1024


def resolve_polystrata_setup_script(root: Path) -> Path | None:
    """Return the PolyStrata setup.py entrypoint across 0.2.16+ (terminal/) and 0.1.x layouts."""
    for rel in (Path("terminal") / "setup.py", Path("setup.py")):
        cand = root / rel
        if cand.is_file() and not cand.is_symlink() and not cand.parent.is_symlink():
            return cand
    return None


def resolve_polystrata_server_script(root: Path) -> Path | None:
    """Return the PolyStrata serve/server.py entrypoint across 0.2.16+ and 0.1.x layouts."""
    for rel in (Path("terminal") / "serve" / "server.py", Path("serve") / "server.py"):
        cand = root / rel
        if cand.is_file() and not cand.is_symlink() and not cand.parent.is_symlink():
            return cand
    return None


def _safe_resolve(path: Path) -> Path | None:
    try:
        return path.resolve(strict=True)
    except OSError:
        return None


def _is_usable_binary(path: Path, system: str) -> bool:
    if not path.is_file():
        return False
    if system == "Windows":
        return True
    return os.access(path, os.X_OK)


def pick_venv_python(
    venv_dir: Path,
    preferred: Path | None = None,
    *,
    platform_system: str | None = None,
) -> Path | None:
    """Locate an executable Python inside a POSIX (bin/) or Windows (Scripts/) venv."""
    system = platform_system or platform.system()
    bin_dir = venv_dir / "bin"
    scripts_dir = venv_dir / "Scripts"
    if system == "Windows":
        search = ((scripts_dir, ("python.exe", "python")),)
    else:
        search = ((bin_dir, ("python", "python3")),)
    allowed_parents = {directory for directory, _ in search}
    candidates: list[Path] = []
    if preferred is not None and preferred.parent in allowed_parents:
        candidates.append(preferred)
    for directory, names in search:
        for name in names:
            candidate = directory / name
            if candidate not in candidates:
                candidates.append(candidate)
    for path in candidates:
        if _is_usable_binary(path, system):
            return path
    return None


def find_polystrata_engine_binaries(
    root: Path, *, platform_system: str | None = None
) -> dict[str, Path]:
    """Return discovered native PolyStrata engine executables keyed by 'poly' / 'strata'."""
    system = platform_system or platform.system()
    found: dict[str, Path] = {}
    if system == "Windows":
        poly_candidates = (
            root / "terminal" / "engine-poly" / "strata-poly.exe",
            root / "engine" / "build-metal" / "strata-poly.exe",
            root / "engine-poly" / "strata-poly.exe",
            root / "build-metal" / "strata-poly.exe",
        )
        strata_candidates = (
            root / "terminal" / "engine" / "strata.exe",
            root / "terminal" / "engine-cuda12" / "strata.exe",
            root / "engine" / "build-metal" / "strata.exe",
            root / "engine" / "strata.exe",
            root / "build-metal" / "strata.exe",
        )
    else:
        poly_candidates = (
            root / "terminal" / "engine-poly" / "strata-poly",
            root / "engine" / "build-metal" / "strata-poly",
            root / "engine-poly" / "strata-poly",
            root / "build-metal" / "strata-poly",
        )
        strata_candidates = (
            root / "terminal" / "engine" / "strata",
            root / "terminal" / "engine-cuda12" / "strata",
            root / "engine" / "build-metal" / "strata",
            root / "engine" / "build-metal" / "strata-metal",
            root / "engine" / "build-metal" / "strata-q36",
            root / "engine" / "strata",
            root / "build-metal" / "strata",
            root / "build-metal" / "strata-metal",
            root / "build-metal" / "strata-q36",
        )
    for role, candidates in (("poly", poly_candidates), ("strata", strata_candidates)):
        for candidate in candidates:
            if candidate.parent.is_symlink() or candidate.parent.parent.is_symlink():
                continue
            if _is_usable_binary(candidate, system):
                resolved = _safe_resolve(candidate)
                if resolved is not None:
                    found[role] = resolved
                    break
    return found


def venv_base_python(executable: Path, *, platform_system: str | None = None) -> Path | None:
    """Resolve the base CPython interpreter referenced by pyvenv.cfg (needed for Windows venvs)."""
    system = platform_system or platform.system()
    cfg_path = executable.parent.parent / "pyvenv.cfg"
    try:
        if not cfg_path.is_file():
            return None
        home_value = None
        for line in cfg_path.read_text(encoding="utf-8").splitlines():
            key, sep, value = line.partition("=")
            if sep and key.strip().lower() == "home":
                home_value = value.strip()
                break
        if not home_value:
            return None
        home_dir = Path(home_value)
        if not home_dir.is_dir():
            return None
        names = ("python.exe", "python") if system == "Windows" else ("python3", "python")
        for name in names:
            candidate = home_dir / name
            if candidate.is_file():
                resolved = _safe_resolve(candidate)
                if resolved is not None:
                    return resolved
    except (OSError, UnicodeError):
        return None
    return None


def validate_polystrata_layout(
    root: Path, executable: Path, *, platform_system: str | None = None
) -> dict[str, Path]:
    """Validate PolyStrata root, venv Python, and at least one usable engine binary."""
    system = platform_system or platform.system()
    if resolve_polystrata_server_script(root) is None:
        raise PreflightError("local_engine_polystrata_server_missing")
    if resolve_polystrata_setup_script(root) is None and not (root / "polystrata.py").is_file():
        raise PreflightError("local_engine_polystrata_layout_invalid")
    if not (executable.parent.parent / "pyvenv.cfg").is_file():
        raise PreflightError("local_engine_python_venv_required")
    if not _is_usable_binary(executable, system):
        raise PreflightError("local_engine_executable_invalid")
    binaries = find_polystrata_engine_binaries(root, platform_system=system)
    if not binaries:
        raise PreflightError("local_engine_polystrata_engine_missing")
    return binaries


def polystrata_runtime_roots(root: Path, executable: Path | None = None) -> list[str]:
    """Return runtime directories to freeze (excluding model weight and C++ source dirs)."""
    dirs: list[Path] = [
        root / "serve",
        root / "terminal" / "serve",
        root / "terminal" / "tools",
        root / "terminal" / "strata_control",
    ]
    if executable is not None and (executable.parent.parent / "pyvenv.cfg").is_file():
        dirs.append(executable.parent.parent)
    elif (root / ".venv" / "pyvenv.cfg").is_file():
        dirs.append(root / ".venv")
    elif (root / "terminal" / ".venv" / "pyvenv.cfg").is_file():
        dirs.append(root / "terminal" / ".venv")
    dirs.extend(
        (
            root / "terminal" / "engine",
            root / "terminal" / "engine-cuda12",
            root / "terminal" / "engine-poly",
            root / "engine" / "build-metal",
            root / "engine" / "build-vision",
            root / "engine-poly",
            root / "build-metal",
            root / "build-vision",
        )
    )
    if not (root / "engine" / "CMakeLists.txt").is_file():
        dirs.append(root / "engine")
    resolved: list[str] = []
    for item in dirs:
        if item.is_dir() and not item.is_symlink() and not item.parent.is_symlink():
            target = _safe_resolve(item)
            if target is not None and str(target) not in resolved:
                resolved.append(str(target))
    if not resolved:
        target = _safe_resolve(root)
        if target is not None:
            resolved.append(str(target))
    return sorted(resolved)


def _read_build_json(root: Path, *, engine_dir: Path | None = None) -> dict | None:
    search_dirs: list[Path] = []
    if engine_dir is not None:
        search_dirs.append(engine_dir)
    search_dirs.extend(
        (
            root / "terminal" / "engine-poly",
            root / "terminal" / "engine",
            root / "terminal" / "engine-cuda12",
            root / "engine" / "build-metal",
            root / "engine-poly",
            root / "engine",
            root / "build-metal",
            root,
        )
    )
    for directory in search_dirs:
        for fname in POLYSTRATA_BUILD_JSON_NAMES:
            build_path = directory / fname
            try:
                if not build_path.is_file() or build_path.stat().st_size > _MAX_BUILD_JSON_BYTES:
                    continue
                data = json.loads(build_path.read_text(encoding="utf-8"))
                if isinstance(data, dict):
                    return data
            except (OSError, ValueError, UnicodeError):
                continue
    return None


def read_build_version(root: Path, *, engine_dir: Path | None = None) -> str | None:
    data = _read_build_json(root, engine_dir=engine_dir)
    if not data:
        return None
    for key in ("version", "release", "tag", "commit"):
        value = data.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return None


def read_build_backend(root: Path, *, engine_dir: Path | None = None) -> str | None:
    data = _read_build_json(root, engine_dir=engine_dir)
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
    term_dir = root / "terminal"
    if term_dir.is_dir() and not term_dir.is_symlink():
        search_dirs.append(term_dir)
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
        and resolve_polystrata_server_script(directory) is not None
        and (
            resolve_polystrata_setup_script(directory) is not None
            or (directory / "polystrata.py").is_file()
        )
    )


def infer_polystrata_root(executable: Path) -> Path | None:
    """Infer PolyStrata working_directory from a .venv Python executable."""
    venv_dir = executable.parent.parent
    if executable.parent.name in ("bin", "Scripts") and (venv_dir / "pyvenv.cfg").is_file():
        candidate = venv_dir.parent
        if candidate.name == "terminal" and is_polystrata_root(candidate.parent):
            return _safe_resolve(candidate.parent)
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
        venv_dir = resolved / "terminal" / ".venv"
    if not (venv_dir / "pyvenv.cfg").is_file():
        return False
    python_exec = pick_venv_python(venv_dir, preferred=preferred_python)
    if python_exec is None:
        return False
    try:
        validate_polystrata_layout(resolved, python_exec)
    except PreflightError:
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
                parent_root = path.parent.parent
                if parent_root.name == "terminal":
                    targets.append((parent_root.parent, None))
                targets.append((parent_root, None))
            elif name in ("setup.py", "polystrata.py", "build.json"):
                if path.parent.name == "terminal":
                    targets.append((path.parent.parent, None))
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
