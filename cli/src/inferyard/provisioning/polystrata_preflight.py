"""Read-only preflight checks and log sanitization for managed PolyStrata setup."""

from __future__ import annotations

from collections.abc import Callable
from functools import lru_cache
from importlib.resources import files
import math
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys

from inferyard.contracts.validation import strict_json_loads
from inferyard.platforms.device_gpu import detect_gpus
from inferyard.provisioning.http_client import validate_proxy_origin
from inferyard.provisioning.preparation_io import PreparationError, linked, native_platform

GIB = 1024**3
MIB = 1024**2
_MIN_MACOS = (15, 0)
_MIN_NVIDIA_DRIVER_MAJOR = 580
_DEFAULT_RUNTIME_DISK_BYTES = 2 * GIB
_RESUME_RUNTIME_DISK_BYTES = 256 * MIB
_DEFAULT_MODEL_DISK_BYTES = 12 * GIB
_GGUF_WORK_DISK_BYTES = 512 * MIB
_SUPPORTED_PLATFORMS = frozenset({("Darwin", "arm64"), ("Linux", "x64"), ("Windows", "x64")})
_VALID_BACKENDS = frozenset({"auto", "cuda", "hip", "metal"})
_VALID_MODEL_SOURCES = frozenset({"auto", "huggingface", "modelscope"})

_VERSION_PART_RE = re.compile(r"\d+")
_ANSI_RE = re.compile(r"\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))")
_URL_CRED_RE = re.compile(r"(https?://)([^/\s:@]+:[^/\s@]*@)", re.IGNORECASE)
_AUTH_HEADER_RE = re.compile(r"\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{6,}", re.IGNORECASE)
_TOKEN_PREFIX_RE = re.compile(r"\b(?:hf_|gh[pousr]_|github_pat_|sk-)[A-Za-z0-9_-]{8,}")
_API_KEY_FLAG_RE = re.compile(r"(--api-key(?:\s+|=))([^\s\"']+)", re.IGNORECASE)
_KV_SECRET_RE = re.compile(
    r"\b(api[_-]?key|access[_-]?token|auth[_-]?token|hf[_-]?token|token)\s*[:=]\s*([^\s,;\"']{6,})",
    re.IGNORECASE,
)


def sanitize_log_line(line: str) -> str:
    """Strip ANSI escape sequences, control characters, and credential patterns."""
    cleaned = _ANSI_RE.sub("", line)
    cleaned = _URL_CRED_RE.sub(r"\1[REDACTED]@", cleaned)
    cleaned = _AUTH_HEADER_RE.sub(r"\1 [REDACTED]", cleaned)
    cleaned = _TOKEN_PREFIX_RE.sub("[REDACTED]", cleaned)
    cleaned = _API_KEY_FLAG_RE.sub(r"\1[REDACTED]", cleaned)
    cleaned = _KV_SECRET_RE.sub(r"\1=[REDACTED]", cleaned)
    return "".join(ch for ch in cleaned if ch == "\t" or (ord(ch) >= 32 and ord(ch) != 127))


@lru_cache(maxsize=1)
def _load_starter_catalog() -> dict:
    raw = files("inferyard").joinpath("data", "starter_catalog.json").read_text(encoding="utf-8")
    return strict_json_loads(raw)


def _matching_catalog_models(family: str | None, model: str | None) -> list[dict]:
    if not family and not model:
        return []
    catalog = _load_starter_catalog()
    models = catalog.get("polystrata_models") or []
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
    return matches


def _entry_disk_bytes(entry: dict, *, system: str, gguf_path: Path | None) -> int:
    if gguf_path is not None:
        if entry.get("model_format") == "strata_pack":
            pack = int(
                (entry.get("mac_pack_bytes") if system == "Darwin" else entry.get("arena_bytes"))
                or 0
            )
            return max(_GGUF_WORK_DISK_BYTES, pack + 2 * GIB)
        return _GGUF_WORK_DISK_BYTES
    download = int(entry.get("download_bytes") or 0)
    if entry.get("model_format") == "strata_pack":
        if system == "Darwin":
            pack = int(entry.get("mac_pack_bytes") or 0)
            return download + pack + 6 * GIB
        arena = int(entry.get("arena_bytes") or 0)
        return download + arena + 8_000_000_000
    draft = int((entry.get("gguf_sources") or {}).get("draft_bytes") or 0)
    return download + draft + 2 * GIB


def _parse_version(raw: str | None) -> tuple[int, ...] | None:
    if not isinstance(raw, str) or not raw.strip():
        return None
    parts = _VERSION_PART_RE.findall(raw.strip())
    if not parts:
        return None
    return tuple(int(part) for part in parts[:3])


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
        usage = disk_usage_fn(ancestor)
        free = int(usage.free)
    except OSError:
        return None, None
    try:
        dev = ancestor.stat().st_dev
    except OSError:
        dev = None
    return free, dev


def _required_model_disk_bytes(
    *,
    system: str,
    family: str | None,
    model: str | None,
    gguf_path: Path | None,
) -> int:
    matches = _matching_catalog_models(family, model)
    if not matches:
        return _GGUF_WORK_DISK_BYTES if gguf_path is not None else _DEFAULT_MODEL_DISK_BYTES
    return min(_entry_disk_bytes(entry, system=system, gguf_path=gguf_path) for entry in matches)


def _default_xcode_clt_check() -> bool:
    try:
        result = subprocess.run(
            ["xcode-select", "-p"],
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return False
    if result.returncode != 0:
        return False
    clt_path = result.stdout.strip()
    return bool(clt_path) and Path(clt_path).is_dir()


def _default_linux_venv_check(python_executable: str) -> bool:
    try:
        help_res = subprocess.run(
            [python_executable, "-m", "venv", "--help"],
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
        if help_res.returncode != 0:
            return False
        ensure_res = subprocess.run(
            [python_executable, "-c", "import venv, ensurepip"],
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
        return ensure_res.returncode == 0
    except (OSError, subprocess.SubprocessError):
        return False


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


def _check_gpu_drivers(
    system: str,
    backend: str | None,
    gpu_probe: Callable[[str], dict] | None,
) -> list[dict]:
    if system not in ("Linux", "Windows"):
        return []
    probe = gpu_probe if gpu_probe is not None else detect_gpus
    gpu_info = probe(system) or {}
    devices = gpu_info.get("devices") or []
    nvidia_devices = [
        dev for dev in devices if str(dev.get("vendor", "")).lower() == "nvidia"
    ]
    amd_devices = [dev for dev in devices if str(dev.get("vendor", "")).lower() == "amd"]

    blockers: list[dict] = []
    if backend == "cuda" and not nvidia_devices:
        blockers.append(
            {
                "code": "polystrata_nvidia_gpu_missing",
                "message": "CUDA backend requested, but no NVIDIA GPU was detected.",
                "fix_command": None,
            }
        )
        return blockers
    if backend == "hip" and not amd_devices:
        blockers.append(
            {
                "code": "polystrata_amd_gpu_missing",
                "message": "HIP backend requested, but no supported AMD GPU was detected.",
                "fix_command": None,
            }
        )
        return blockers
    if backend != "hip" and nvidia_devices:
        parsed_majors = []
        for dev in nvidia_devices:
            ver = _parse_version(dev.get("driver_version"))
            if ver:
                parsed_majors.append(ver[0])
        if parsed_majors and max(parsed_majors) < _MIN_NVIDIA_DRIVER_MAJOR:
            blockers.append(
                {
                    "code": "polystrata_nvidia_driver_too_old",
                    "message": (
                        f"NVIDIA driver {max(parsed_majors)} is below the required "
                        f"{_MIN_NVIDIA_DRIVER_MAJOR}+ for PolyStrata CUDA 13."
                    ),
                    "fix_command": None,
                }
            )
    return blockers


def run_polystrata_preflight(
    out: Path,
    data_dir: Path,
    *,
    family: str | None = None,
    model: str | None = None,
    context: int | None = None,
    backend: str | None = None,
    model_source: str | None = None,
    gguf_path: Path | None = None,
    proxy_origin: str | None = None,
    timeout_seconds: float = 3600,
    resume: bool = False,
    platform_info: tuple[str, str] | None = None,
    macos_version: str | None = None,
    xcode_clt_check: Callable[[], bool] | None = None,
    linux_venv_check: Callable[[str], bool] | None = None,
    gpu_probe: Callable[[str], dict] | None = None,
    disk_usage_fn=shutil.disk_usage,
    venv_python: str | None = None,
) -> dict:
    """Validate prerequisites read-only; raise PreparationError with structured blockers on failure."""
    validate_proxy_origin(proxy_origin)
    if (
        type(timeout_seconds) not in (int, float)
        or not math.isfinite(timeout_seconds)
        or timeout_seconds <= 0
    ):
        raise PreparationError("invalid_input")
    if context is not None and (type(context) is not int or not 1 <= context <= 524288):
        raise PreparationError("invalid_input")
    if backend is not None and backend not in _VALID_BACKENDS:
        raise PreparationError("invalid_input")
    if model_source is not None and model_source not in _VALID_MODEL_SOURCES:
        raise PreparationError("invalid_input")
    if os.path.lexists(data_dir) and (linked(data_dir) or not data_dir.is_dir()):
        raise PreparationError("invalid_input")

    system, arch = platform_info if platform_info is not None else native_platform()
    blockers: list[dict] = []

    if (system, arch) not in _SUPPORTED_PLATFORMS:
        blockers.append(
            {
                "code": "polystrata_platform_unsupported",
                "message": f"PolyStrata managed setup does not support {system}/{arch}.",
                "fix_command": None,
            }
        )
    elif backend is not None and backend != "auto":
        if system == "Darwin" and backend != "metal":
            blockers.append(
                {
                    "code": "polystrata_backend_unsupported",
                    "message": f"Backend '{backend}' is not supported on macOS (use metal or auto).",
                    "fix_command": None,
                }
            )
        elif system != "Darwin" and backend == "metal":
            blockers.append(
                {
                    "code": "polystrata_backend_unsupported",
                    "message": "Metal backend is only supported on Apple Silicon macOS.",
                    "fix_command": None,
                }
            )

    if gguf_path is not None:
        blockers.extend(_validate_gguf_path(gguf_path))

    if system == "Darwin" and arch == "arm64":
        raw_ver = macos_version if macos_version is not None else platform.mac_ver()[0]
        parsed_ver = _parse_version(raw_ver)
        if parsed_ver is None or parsed_ver < _MIN_MACOS:
            blockers.append(
                {
                    "code": "polystrata_macos_too_old",
                    "message": "macOS 15.0 or newer is required for PolyStrata Metal.",
                    "fix_command": None,
                }
            )
        clt_fn = xcode_clt_check if xcode_clt_check is not None else _default_xcode_clt_check
        if not clt_fn():
            blockers.append(
                {
                    "code": "polystrata_xcode_clt_missing",
                    "message": "Xcode Command Line Tools are required to build PolyStrata on macOS.",
                    "fix_command": "xcode-select --install",
                }
            )

    if system == "Linux" and arch == "x64":
        py_cmd = venv_python or shutil.which("python3") or sys.executable
        venv_fn = linux_venv_check if linux_venv_check is not None else _default_linux_venv_check
        if not venv_fn(py_cmd):
            blockers.append(
                {
                    "code": "polystrata_python_venv_missing",
                    "message": "Python venv support (with ensurepip) is required on Linux.",
                    "fix_command": "sudo apt install python3-venv",
                }
            )

    blockers.extend(_check_gpu_drivers(system, backend, gpu_probe))

    runtime_already_extracted = (
        resume and (out / "setup.py").is_file() and (out / "serve" / "server.py").is_file()
    )
    runtime_needed = _RESUME_RUNTIME_DISK_BYTES if runtime_already_extracted else _DEFAULT_RUNTIME_DISK_BYTES
    model_needed = _required_model_disk_bytes(
        system=system, family=family, model=model, gguf_path=gguf_path
    )
    out_free, out_dev = _disk_free_bytes(out, disk_usage_fn=disk_usage_fn)
    data_free, data_dev = _disk_free_bytes(data_dir, disk_usage_fn=disk_usage_fn)

    if out_free is not None and data_free is not None and out_dev is not None and out_dev == data_dev:
        combined_needed = runtime_needed + model_needed
        if out_free < combined_needed:
            blockers.append(
                {
                    "code": "polystrata_disk_insufficient",
                    "message": (
                        f"Insufficient free disk space: {out_free / GIB:.1f} GiB available, "
                        f"at least {combined_needed / GIB:.1f} GiB required."
                    ),
                    "required_bytes": combined_needed,
                    "free_bytes": out_free,
                    "fix_command": None,
                }
            )
    else:
        if out_free is not None and out_free < runtime_needed:
            blockers.append(
                {
                    "code": "polystrata_disk_insufficient",
                    "message": (
                        f"Insufficient disk space at --out: {out_free / GIB:.1f} GiB available, "
                        f"at least {runtime_needed / GIB:.1f} GiB required."
                    ),
                    "required_bytes": runtime_needed,
                    "free_bytes": out_free,
                    "fix_command": None,
                }
            )
        if data_free is not None and data_free < model_needed:
            blockers.append(
                {
                    "code": "polystrata_disk_insufficient",
                    "message": (
                        f"Insufficient disk space at --data-dir: {data_free / GIB:.1f} GiB available, "
                        f"at least {model_needed / GIB:.1f} GiB required."
                    ),
                    "required_bytes": model_needed,
                    "free_bytes": data_free,
                    "fix_command": None,
                }
            )

    if blockers:
        primary = blockers[0]["code"]
        raise PreparationError(
            primary,
            code=2,
            details={
                "stage": "preflight",
                "failure_stage": "preflight",
                "failure_cause": primary,
                "blockers": blockers,
            },
        )

    return {
        "platform": system,
        "architecture": arch,
        "out_free_bytes": out_free,
        "data_free_bytes": data_free,
        "runtime_required_bytes": runtime_needed,
        "model_required_bytes": model_needed,
        "blockers": [],
    }
