"""Read-only local engine detection and validated installation scaffolding."""

from __future__ import annotations

import errno
import json
import os
import platform
import shutil
import tempfile
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES, LOCAL_FAMILIES
from inferyard.errors import PreflightError
from inferyard.platforms.local_engines.detect_polystrata import (
    collect_polystrata_targets,
    infer_polystrata_root,
    normalize_polystrata_root,
    polystrata_runtime_roots,
    probe_polystrata_root,
)
from inferyard.platforms.local_engines.installation import description

DETECT_DEFINITION = "local-engine-detect.v1"
_STANDARD_LLAMA_PATHS = (
    Path("/opt/homebrew/bin/llama-server"),
    Path("/usr/local/bin/llama-server"),
)
_PYTHON_PACKAGE_FAMILIES = ("mlx-lm", "vllm", "sglang")


def _safe_resolve(path: Path) -> Path | None:
    try:
        return path.resolve(strict=True)
    except OSError:
        return None


def _is_standalone_executable(path: Path) -> Path | None:
    resolved = _safe_resolve(path)
    if resolved is None or not resolved.is_file():
        return None
    return resolved


def _binary_runtime_roots(executable: Path, resolved: Path) -> list[str]:
    roots = [str(resolved.parent)]
    if executable.is_symlink() and resolved.parent.name == "bin":
        sibling_lib = resolved.parent.parent / "lib"
        if sibling_lib.is_dir() and not sibling_lib.is_symlink():
            lib_resolved = _safe_resolve(sibling_lib)
            if lib_resolved is not None and str(lib_resolved) not in roots:
                roots.append(str(lib_resolved))
    return roots


def _read_pyvenv_cfg(cfg_path: Path) -> dict[str, str] | None:
    try:
        if not cfg_path.is_file():
            return None
        values: dict[str, str] = {}
        for line in cfg_path.read_text(encoding="utf-8").splitlines():
            key, sep, value = line.partition("=")
            if sep:
                norm_key = key.strip().lower()
                if norm_key in values:
                    return None
                values[norm_key] = value.strip()
        return values
    except (OSError, UnicodeError):
        return None


def _venv_python_dir_name(venv_dir: Path, cfg: dict[str, str] | None) -> str | None:
    if cfg:
        for key in ("version", "version_info"):
            raw_ver = cfg.get(key, "").strip()
            parts = raw_ver.split(".")
            if len(parts) >= 2 and parts[0].isdigit() and parts[1].isdigit():
                return f"python{parts[0]}.{parts[1]}"
    names: set[str] = set()
    for lib_name in ("lib", "lib64"):
        lib_dir = venv_dir / lib_name
        if not lib_dir.is_dir():
            continue
        try:
            with os.scandir(lib_dir) as entries:
                for entry in entries:
                    if entry.name.startswith("python") and entry.is_dir():
                        names.add(entry.name)
        except OSError:
            continue
    return next(iter(names)) if len(names) == 1 else None


def _env_site_dirs(env_dir: Path, *, python_dir_name: str | None = None) -> list[Path]:
    found: list[Path] = []
    win_site = env_dir / "Lib" / "site-packages"
    if win_site.is_dir():
        found.append(win_site)
    for lib_name in ("lib", "lib64"):
        lib_dir = env_dir / lib_name
        if not lib_dir.is_dir():
            continue
        try:
            with os.scandir(lib_dir) as entries:
                for entry in entries:
                    if not entry.name.startswith("python") or not entry.is_dir():
                        continue
                    if python_dir_name is not None and entry.name != python_dir_name:
                        continue
                    site = Path(entry.path) / "site-packages"
                    if site.is_dir():
                        found.append(site)
        except OSError:
            continue
    return sorted(found)


def _site_packages_dirs(venv_dir: Path) -> list[Path]:
    found = _env_site_dirs(venv_dir)
    cfg = _read_pyvenv_cfg(venv_dir / "pyvenv.cfg")
    if cfg and cfg.get("include-system-site-packages", "").lower() == "true":
        home_raw = cfg.get("home", "")
        if home_raw:
            home_path = Path(home_raw)
            if home_path.is_absolute():
                py_dir_name = _venv_python_dir_name(venv_dir, cfg)
                roots = [home_path]
                if home_path.name.lower() in ("bin", "scripts"):
                    roots.insert(0, home_path.parent)
                for base_root in roots:
                    for extra in _env_site_dirs(base_root, python_dir_name=py_dir_name):
                        if extra not in found:
                            found.append(extra)
    return found


def _has_package_in_site(site: Path, family: str) -> bool:
    if family == "mlx-lm":
        return (site / "mlx_lm" / "__init__.py").is_file() or (site / "mlx_lm.py").is_file()
    if family == "vllm":
        return (
            (site / "vllm" / "entrypoints" / "openai" / "api_server.py").is_file()
            or (site / "vllm" / "entrypoints" / "openai" / "api_server" / "__init__.py").is_file()
            or (site / "vllm" / "__init__.py").is_file()
        )
    if family == "sglang":
        return (
            (site / "sglang" / "launch_server.py").is_file()
            or (site / "sglang" / "launch_server" / "__init__.py").is_file()
            or (site / "sglang" / "__init__.py").is_file()
        )
    return False


def _is_python_binary_name(name: str) -> bool:
    low = name.lower()
    return low.startswith("python") and "config" not in low


def _pick_venv_python(venv_dir: Path, preferred: Path | None = None) -> Path | None:
    candidates: list[Path] = []
    allowed_dirs = {venv_dir / "bin", venv_dir / "Scripts", venv_dir}
    if (
        preferred is not None
        and preferred.parent in allowed_dirs
        and _is_python_binary_name(preferred.name)
    ):
        candidates.append(preferred)
    for candidate in (
        venv_dir / "bin" / "python",
        venv_dir / "bin" / "python3",
        venv_dir / "Scripts" / "python.exe",
        venv_dir / "Scripts" / "python",
        venv_dir / "python.exe",
    ):
        if candidate not in candidates:
            candidates.append(candidate)
    existing = [p for p in candidates if p.is_file()]
    if not existing:
        return None
    for p in existing:
        if os.access(p, os.X_OK):
            return p
    return existing[0]


def _is_python_env_dir(directory: Path) -> bool:
    return (
        (directory / "pyvenv.cfg").is_file()
        or (directory / "conda-meta").is_dir()
        or ((directory / "bin" / "python").is_file() and bool(_site_packages_dirs(directory)))
        or ((directory / "Scripts" / "python.exe").is_file() and bool(_site_packages_dirs(directory)))
    )


def _probe_python_venv(
    venv_dir: Path,
    seen: set[tuple[str, str]],
    out: list[dict],
    *,
    preferred_python: Path | None = None,
) -> None:
    resolved_venv = _safe_resolve(venv_dir)
    if resolved_venv is None or not resolved_venv.is_dir():
        return
    python_exec = _pick_venv_python(resolved_venv, preferred=preferred_python)
    if python_exec is None:
        return
    sites = _site_packages_dirs(resolved_venv)
    if not sites:
        return
    for family in _PYTHON_PACKAGE_FAMILIES:
        if not any(_has_package_in_site(site, family) for site in sites):
            continue
        dedupe_key = (family, str(resolved_venv))
        if dedupe_key in seen:
            continue
        seen.add(dedupe_key)
        out.append(
            {
                "family": family,
                "executable": str(python_exec),
                "working_directory": str(resolved_venv),
                "runtime_roots": [str(resolved_venv)],
                "models_root": None,
                "label": f"{family} · {resolved_venv.name} ({resolved_venv})",
            }
        )


def _shebang_venv(script_path: Path) -> Path | None:
    try:
        if not script_path.is_file():
            return None
        with script_path.open("rb") as stream:
            first_line = stream.readline(256)
        if not first_line.startswith(b"#!"):
            return None
        interp = first_line[2:].decode("utf-8", errors="ignore").strip().split()[0]
        interp_path = Path(interp)
        if interp_path.is_absolute():
            if _is_python_env_dir(interp_path.parent.parent):
                return interp_path.parent.parent
            if _is_python_env_dir(interp_path.parent):
                return interp_path.parent
    except (OSError, IndexError):
        return None
    return None


def _collect_extra_targets(
    extra_paths: tuple | list,
) -> tuple[list[Path], list[Path], list[tuple[Path, Path | None]]]:
    llama_paths: list[Path] = []
    lms_paths: list[Path] = []
    venv_targets: list[tuple[Path, Path | None]] = []

    def inspect_dir(directory: Path, recurse_children: bool) -> None:
        if _is_python_env_dir(directory):
            venv_targets.append((directory, None))
            return
        for bin_candidate in (directory / "llama-server", directory / "bin" / "llama-server"):
            if bin_candidate.is_file():
                llama_paths.append(bin_candidate)
        for lms_candidate in (directory / "lms", directory / "bin" / "lms"):
            if lms_candidate.is_file():
                lms_paths.append(lms_candidate)
        for py_candidate in (
            directory / "python",
            directory / "bin" / "python",
            directory / "Scripts" / "python.exe",
            directory / "python.exe",
        ):
            if py_candidate.is_file():
                env_root = (
                    py_candidate.parent.parent
                    if py_candidate.parent.name in ("bin", "Scripts")
                    else py_candidate.parent
                )
                if _is_python_env_dir(env_root):
                    venv_targets.append((env_root, py_candidate))
        if recurse_children:
            try:
                with os.scandir(directory) as entries:
                    for entry in sorted(entries, key=lambda item: item.name):
                        if entry.is_dir():
                            child = Path(entry.path)
                            if _is_python_env_dir(child):
                                venv_targets.append((child, None))
            except OSError:
                pass

    for raw in extra_paths:
        if not raw:
            continue
        path = Path(raw)
        if path.is_file():
            name = path.name.lower()
            if name == "llama-server":
                llama_paths.append(path)
            elif name == "lms":
                lms_paths.append(path)
            elif _is_python_binary_name(name):
                env_root = (
                    path.parent.parent
                    if path.parent.name in ("bin", "Scripts")
                    else path.parent
                )
                venv_targets.append((env_root, path))
            elif _is_python_env_dir(path.parent.parent):
                venv_targets.append((path.parent.parent, None))
            elif _is_python_env_dir(path.parent):
                venv_targets.append((path.parent, None))
        elif path.is_dir():
            inspect_dir(path, recurse_children=True)
    return llama_paths, lms_paths, venv_targets


def _child_dirs(parent: Path) -> list[Path]:
    if not parent.is_dir():
        return []
    try:
        with os.scandir(parent) as entries:
            return [Path(entry.path) for entry in sorted(entries, key=lambda item: item.name) if entry.is_dir()]
    except OSError:
        return []


def detect_local_engines(extra_paths=()) -> dict:
    """Probe common macOS/Unix local engine installations read-only."""
    home = Path.home()
    extra_llama, extra_lms, extra_venvs = _collect_extra_targets(tuple(extra_paths or ()))

    candidates: list[dict] = []
    seen_binaries: set[tuple[str, str]] = set()

    llama_candidates = [
        *_STANDARD_LLAMA_PATHS,
        *extra_llama,
    ]
    if which_llama := shutil.which("llama-server"):
        llama_candidates.append(Path(which_llama))
    for path in llama_candidates:
        resolved = _is_standalone_executable(path)
        if resolved is None:
            continue
        key = ("llama-cpp", str(resolved))
        if key in seen_binaries:
            continue
        seen_binaries.add(key)
        candidates.append(
            {
                "family": "llama-cpp",
                "executable": str(resolved),
                "working_directory": str(resolved.parent),
                "runtime_roots": _binary_runtime_roots(path, resolved),
                "models_root": None,
                "label": f"llama-cpp · {resolved}",
            }
        )

    lmstudio_models_dir = home / ".lmstudio" / "models"
    lmstudio_models = (
        str(lmstudio_models_dir.resolve()) if lmstudio_models_dir.is_dir() else None
    )
    lms_candidates = [home / ".lmstudio" / "bin" / "lms", *extra_lms]
    if which_lms := shutil.which("lms"):
        lms_candidates.append(Path(which_lms))
    for path in lms_candidates:
        resolved = _is_standalone_executable(path)
        if resolved is None:
            continue
        key = ("lmstudio", str(resolved))
        if key in seen_binaries:
            continue
        seen_binaries.add(key)
        candidates.append(
            {
                "family": "lmstudio",
                "executable": str(resolved),
                "working_directory": str(resolved.parent),
                "runtime_roots": _binary_runtime_roots(path, resolved),
                "models_root": lmstudio_models,
                "label": f"lmstudio · {resolved}",
            }
        )

    venv_targets: list[tuple[Path, Path | None]] = [*extra_venvs]
    if active_venv := os.environ.get("VIRTUAL_ENV"):
        venv_targets.append((Path(active_venv), None))
    if conda_prefix := os.environ.get("CONDA_PREFIX"):
        venv_targets.append((Path(conda_prefix), None))
    cwd = Path.cwd()
    venv_targets.extend([(cwd / ".venv", None), (cwd / "venv", None)])
    for tool_root in (
        home / ".local" / "share" / "uv" / "tools",
        home / ".local" / "pipx" / "venvs",
        home / "Library" / "Application Support" / "pipx" / "venvs",
        home / ".venvs",
        home / "venvs",
        home / "miniconda3" / "envs",
        home / "anaconda3" / "envs",
        home / "miniforge3" / "envs",
        home / "mambaforge" / "envs",
    ):
        for child in _child_dirs(tool_root):
            venv_targets.append((child, None))
    for tool_cmd in ("mlx_lm.server", "vllm"):
        if cmd_path := shutil.which(tool_cmd):
            cmd = Path(cmd_path)
            if (cmd.parent.parent / "pyvenv.cfg").is_file():
                venv_targets.append((cmd.parent.parent, None))
            elif shebang_root := _shebang_venv(cmd):
                venv_targets.append((shebang_root, None))

    seen_venvs: set[tuple[str, str]] = set()
    for venv_dir, preferred in venv_targets:
        _probe_python_venv(venv_dir, seen_venvs, candidates, preferred_python=preferred)

    seen_polystrata: set[tuple[str, str]] = set()
    for ps_root, preferred in collect_polystrata_targets(tuple(extra_paths or ()), home, cwd):
        probe_polystrata_root(ps_root, seen_polystrata, candidates, preferred_python=preferred)

    candidates.sort(key=lambda item: (item["family"], item["executable"]))
    return {
        "definition": DETECT_DEFINITION,
        "candidates": candidates,
        "model_requests_sent": 0,
    }


def _require_existing_path(raw: str | Path, missing_reason: str) -> Path:
    path = Path(raw)
    if not path.is_absolute():
        raise PreflightError("local_engine_absolute_path_required")
    if not path.exists():
        raise PreflightError(missing_reason)
    return path


def build_installation(
    family: str,
    executable: str | Path,
    *,
    working_directory: str | Path | None = None,
    runtime_roots: list[str | Path] | tuple[str | Path, ...] | None = None,
    models_root: str | Path | None = None,
    data_dir: str | Path | None = None,
) -> dict:
    """Build and validate a local-engine-installation.v1 declaration dict."""
    if family not in LOCAL_FAMILIES:
        raise PreflightError("local_engine_direct_family_invalid")
    if not executable:
        raise PreflightError("local_engine_locator_missing")
    exe_path = _require_existing_path(executable, "local_engine_locator_missing")
    if not exe_path.is_file():
        raise PreflightError("local_engine_executable_invalid")

    if family in DIRECTORY_FAMILIES or family == "polystrata":
        parent_resolved = _safe_resolve(exe_path.parent)
        if parent_resolved is None:
            raise PreflightError("local_engine_locator_missing")
        declared_executable = os.path.normpath(str(parent_resolved / exe_path.name))
    else:
        declared_executable = os.path.normpath(str(exe_path))

    if working_directory is None:
        inferred = infer_polystrata_root(exe_path) if family == "polystrata" else None
        cwd_resolved = inferred if inferred is not None else _safe_resolve(exe_path.parent)
        if cwd_resolved is None or not cwd_resolved.is_dir():
            raise PreflightError("local_engine_cwd_invalid")
        declared_cwd = str(cwd_resolved)
    else:
        cwd_path = _require_existing_path(working_directory, "local_engine_locator_missing")
        if not cwd_path.is_dir():
            raise PreflightError("local_engine_cwd_invalid")
        if family == "polystrata":
            cwd_path = _safe_resolve(normalize_polystrata_root(cwd_path)) or cwd_path
        declared_cwd = os.path.normpath(str(cwd_path))

    if runtime_roots is None:
        if family in DIRECTORY_FAMILIES:
            venv_candidate = (
                exe_path.parent.parent
                if (exe_path.parent.parent / "pyvenv.cfg").is_file()
                or exe_path.parent.name in ("bin", "Scripts")
                else exe_path.parent
            )
            root_resolved = _safe_resolve(venv_candidate)
            if root_resolved is None or not root_resolved.is_dir():
                raise PreflightError("local_engine_runtime_root_invalid")
            declared_roots = [str(root_resolved)]
        elif family == "polystrata":
            declared_roots = polystrata_runtime_roots(Path(declared_cwd), exe_path)
        else:
            resolved_exe = _safe_resolve(exe_path)
            if resolved_exe is None:
                raise PreflightError("local_engine_locator_missing")
            declared_roots = _binary_runtime_roots(exe_path, resolved_exe)
    else:
        declared_roots = [os.path.normpath(str(item)) for item in runtime_roots]

    if (family == "lmstudio") != (models_root is not None):
        raise PreflightError("local_engine_models_root_required_for_lmstudio_only")
    if data_dir is not None and family != "polystrata":
        raise PreflightError("local_engine_data_dir_polystrata_only")

    if family == "sglang" and platform.system() == "Darwin":
        environment = {"SGLANG_USE_MLX": "1"}
    elif family == "polystrata":
        environment = {"PYTHONDONTWRITEBYTECODE": "1"}
    else:
        environment = {}
    payload: dict = {
        "schema_version": SCHEMA_VERSION,
        "definition": "local-engine-installation.v1",
        "family": family,
        "executable": declared_executable,
        "working_directory": declared_cwd,
        "runtime_roots": declared_roots,
        "environment": environment,
    }
    if models_root is not None:
        models_path = _require_existing_path(models_root, "local_engine_locator_missing")
        if not models_path.is_dir():
            raise PreflightError("local_engine_runtime_root_invalid")
        payload["models_root"] = os.path.normpath(str(models_path))
    if data_dir is not None:
        data_path = _require_existing_path(data_dir, "local_engine_locator_missing")
        if not data_path.is_dir():
            raise PreflightError("local_engine_data_dir_invalid")
        payload["data_dir"] = os.path.normpath(str(data_path))
    return description(payload)


def write_installation(out_path: str | Path, payload: dict) -> str:
    """Atomically write a validated installation declaration to a new path (no overwrite)."""
    validated = description(payload)
    target = Path(out_path).absolute()
    if os.path.lexists(target):
        raise FileExistsError("output_exists")
    target.parent.mkdir(parents=True, exist_ok=True)
    parent = target.parent.resolve(strict=True)
    if not parent.is_dir():
        raise NotADirectoryError(parent)
    final_target = parent / target.name
    if os.path.lexists(final_target):
        raise FileExistsError("output_exists")

    encoded = (json.dumps(validated, ensure_ascii=False, indent=2, allow_nan=False) + "\n").encode(
        "utf-8"
    )
    fd, tmp_name = tempfile.mkstemp(dir=parent, prefix=".installation-", suffix=".tmp")
    tmp_path = Path(tmp_name)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(encoded)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(tmp_path, 0o600)
        try:
            os.link(tmp_path, final_target)
        except OSError as exc:
            if exc.errno == errno.EEXIST:
                raise FileExistsError("output_exists") from exc
            if exc.errno in (errno.EXDEV, errno.EPERM, errno.ENOTSUP, errno.EOPNOTSUPP):
                flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
                if hasattr(os, "O_NOFOLLOW"):
                    flags |= os.O_NOFOLLOW
                out_fd = os.open(final_target, flags, 0o600)
                with os.fdopen(out_fd, "wb") as out_stream:
                    out_stream.write(encoded)
                    out_stream.flush()
                    os.fsync(out_stream.fileno())
            else:
                raise
    finally:
        try:
            tmp_path.unlink(missing_ok=True)
        except OSError:
            pass
    return str(final_target)
