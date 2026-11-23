"""Read-only preflight checks and log sanitization for managed PolyStrata setup."""

from __future__ import annotations

from collections.abc import Callable
import math
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess

from inferyard.platforms.device_gpu import detect_gpus
from inferyard.platforms.local_engines.detect_polystrata import pick_venv_python
from inferyard.provisioning.http_client import validate_proxy_origin
from inferyard.provisioning.polystrata_preflight_checks import (
    _DEFAULT_RUNTIME_DISK_BYTES,
    _RESUME_RUNTIME_DISK_BYTES,
    GIB,
    _check_credentialed_env_urls,
    _detect_sibling_migration_risk,
    _disk_free_bytes,
    _required_model_disk_bytes,
    _resolve_macos_gguf_choice,
    _validate_gguf_path,
)
from inferyard.provisioning.polystrata_python import (
    check_python_venv,
    is_supported_python_version,
    probe_python_version,
    resolve_polystrata_venv_creator,
)
from inferyard.provisioning.preparation_io import PreparationError, linked, native_platform

_MIN_MACOS = (15, 0)
_MIN_NVIDIA_DRIVER_MAJOR = 580
_SUPPORTED_PLATFORMS = frozenset({("Darwin", "arm64"), ("Linux", "x64"), ("Windows", "x64")})
_VALID_BACKENDS = frozenset({"auto", "cuda", "hip", "metal"})
_VALID_MODEL_SOURCES = frozenset({"auto", "huggingface", "modelscope"})
_LINUX_SOURCE_BUILD_FAMILIES = frozenset(
    {
        "glm",
        "qwen36",
        "flint",
        "gemma4",
        "lfm25",
        "ornith",
        "tiel",
        "minicpmv",
        "clover",
        "ornith122",
        "deepseek",
        "kolibri",
    }
)

_VERSION_PART_RE = re.compile(r"\d+")
_ANSI_RE = re.compile(r"\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))")
_URL_CRED_RE = re.compile(r"(https?://)([^/\s@]+@)", re.IGNORECASE)
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


def _parse_version(raw: str | None) -> tuple[int, ...] | None:
    if not isinstance(raw, str) or not raw.strip():
        return None
    parts = _VERSION_PART_RE.findall(raw.strip())
    if not parts:
        return None
    return tuple(int(part) for part in parts[:3])


def _default_xcode_clt_check() -> bool:
    try:
        result = subprocess.run(
            ["xcode-select", "-p"], capture_output=True, text=True, timeout=10, check=False
        )
    except (OSError, subprocess.SubprocessError):
        return False
    clt_path = result.stdout.strip()
    return result.returncode == 0 and bool(clt_path) and Path(clt_path).is_dir()


def _default_linux_venv_check(python_executable: str) -> bool:
    return check_python_venv(python_executable)


_NVCC_VER_RE = re.compile(r"release\s+(\d+)\.(\d+)", re.IGNORECASE)


def _find_nvcc() -> Path | None:
    found = shutil.which("nvcc")
    if found:
        return Path(found)
    for base in (Path("/usr/local"), Path("/opt")):
        if base.is_dir():
            try:
                for child in sorted(base.iterdir(), reverse=True):
                    cand = child / "bin" / "nvcc"
                    if child.name.startswith("cuda") and cand.is_file():
                        return cand
            except OSError:
                pass
    return None


def _nvcc_version(nvcc_path: Path) -> tuple[int, int] | None:
    try:
        res = subprocess.run(
            [str(nvcc_path), "--version"], capture_output=True, text=True, timeout=10, check=False
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if res.returncode != 0:
        return None
    match = _NVCC_VER_RE.search(res.stdout + "\n" + res.stderr)
    if match is None:
        return None
    return int(match.group(1)), int(match.group(2))


def _default_linux_build_tools_check(
    *, need_nvcc: bool, min_cuda: tuple[int, int] = (12, 0)
) -> list[str]:
    missing: list[str] = []
    if shutil.which("g++") is None:
        missing.append("g++")
    if need_nvcc:
        nvcc_path = _find_nvcc()
        if nvcc_path is None:
            missing.append("nvcc")
        else:
            ver = _nvcc_version(nvcc_path)
            if ver is None or ver < min_cuda:
                missing.append(f"nvcc>={min_cuda[0]}.{min_cuda[1]}")
    return missing


def _check_gpu_and_build_tools(
    system: str,
    backend: str | None,
    family: str | None,
    gpu_probe: Callable[[str], dict] | None,
    build_tools_check: Callable[[bool], list[str]] | None,
) -> list[dict]:
    if system not in ("Linux", "Windows"):
        return []
    probe = gpu_probe if gpu_probe is not None else detect_gpus
    gpu_info = probe(system) or {}
    devices = gpu_info.get("devices") or []
    nvidia_devices = [dev for dev in devices if str(dev.get("vendor", "")).lower() == "nvidia"]
    amd_devices = [dev for dev in devices if str(dev.get("vendor", "")).lower() == "amd"]

    blockers: list[dict] = []
    if backend == "cuda" and not nvidia_devices:
        return [
            {
                "code": "polystrata_nvidia_gpu_missing",
                "message": "CUDA backend requested, but no NVIDIA GPU was detected.",
                "fix_command": None,
            }
        ]
    if backend == "hip" and not amd_devices:
        return [
            {
                "code": "polystrata_amd_gpu_missing",
                "message": "HIP backend requested, but no supported AMD GPU was detected.",
                "fix_command": None,
            }
        ]
    if backend in (None, "auto") and not nvidia_devices and not amd_devices:
        return [
            {
                "code": "polystrata_gpu_missing",
                "message": "No supported NVIDIA or AMD GPU was detected for PolyStrata.",
                "fix_command": None,
            }
        ]

    using_cuda = backend == "cuda" or (backend != "hip" and bool(nvidia_devices))
    using_hip = backend == "hip" or (backend != "cuda" and not nvidia_devices and bool(amd_devices))
    if using_cuda:
        parsed_majors = []
        for dev in nvidia_devices:
            ver = _parse_version(dev.get("driver_version"))
            if ver:
                parsed_majors.append(ver[0])
        if not parsed_majors:
            blockers.append(
                {
                    "code": "polystrata_nvidia_driver_unverified",
                    "message": "NVIDIA GPU driver version could not be verified.",
                    "fix_command": None,
                }
            )
        elif max(parsed_majors) < _MIN_NVIDIA_DRIVER_MAJOR:
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

    if system == "Linux":
        fam_lower = (family or "").strip().lower()
        old_cuda = False
        has_sm120 = False
        for dev in nvidia_devices:
            cc = _parse_version(str(dev.get("compute_capability") or ""))
            if cc and cc < (7, 5):
                old_cuda = True
            if cc and cc >= (12, 0):
                has_sm120 = True
        min_cuda = (12, 8) if has_sm120 else (12, 0)
        needs_hip_build = using_hip
        needs_cuda_build = using_cuda and (
            fam_lower in _LINUX_SOURCE_BUILD_FAMILIES or old_cuda
        )
        if needs_hip_build or needs_cuda_build:
            checker = (
                build_tools_check
                if build_tools_check is not None
                else (
                    lambda need_nvcc: _default_linux_build_tools_check(
                        need_nvcc=need_nvcc, min_cuda=min_cuda
                    )
                )
            )
            missing_tools = checker(needs_cuda_build)
            if missing_tools:
                fix = (
                    "sudo apt install build-essential"
                    if "g++" in missing_tools
                    else None
                )
                blockers.append(
                    {
                        "code": "polystrata_build_tools_missing",
                        "message": (
                            "Missing Linux build tools required to compile PolyStrata without "
                            f"interactive sudo: {', '.join(missing_tools)}."
                        ),
                        "fix_command": fix,
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
    build_tools_check: Callable[[bool], list[str]] | None = None,
    disk_usage_fn=shutil.disk_usage,
    venv_python: str | None = None,
    env: dict[str, str] | None = None,
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
    resolved_family = family
    resolved_model = model

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

    blockers.extend(_check_credentialed_env_urls(dict(os.environ) if env is None else env))
    blockers.extend(_detect_sibling_migration_risk(out, data_dir))

    if gguf_path is not None:
        gguf_blockers = _validate_gguf_path(gguf_path)
        blockers.extend(gguf_blockers)
        if not gguf_blockers and system == "Darwin":
            resolved_family, resolved_model, match_blockers = _resolve_macos_gguf_choice(
                gguf_path, family, model
            )
            blockers.extend(match_blockers)

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

    resolved_venv_creator = None
    reusable_existing_venv = False
    if (
        resume
        and venv_python is None
        and os.path.lexists(out)
        and not linked(out)
        and (out / ".inferyard-runtime.json").is_file()
        and (out / ".venv" / "pyvenv.cfg").is_file()
        and not linked(out / ".venv")
    ):
        existing_venv_py = pick_venv_python(out / ".venv", platform_system=system)
        if existing_venv_py is not None and is_supported_python_version(
            probe_python_version(str(existing_venv_py))
        ):
            reusable_existing_venv = True
    if (system, arch) in _SUPPORTED_PLATFORMS and not reusable_existing_venv:
        resolved_venv_creator, py_blockers = resolve_polystrata_venv_creator(
            system=system,
            venv_python=venv_python,
            env=env,
            venv_check=linux_venv_check,
        )
        blockers.extend(py_blockers)

    blockers.extend(
        _check_gpu_and_build_tools(system, backend, resolved_family, gpu_probe, build_tools_check)
    )

    runtime_already_extracted = (
        resume
        and (out / ".inferyard-runtime.json").is_file()
        and (out / "setup.py").is_file()
        and (out / "serve" / "server.py").is_file()
    )
    runtime_needed = _RESUME_RUNTIME_DISK_BYTES if runtime_already_extracted else _DEFAULT_RUNTIME_DISK_BYTES
    model_needed = _required_model_disk_bytes(
        system=system,
        family=resolved_family,
        model=resolved_model,
        gguf_path=gguf_path,
        resume=resume,
        data_dir=data_dir,
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
        "resolved_family": resolved_family,
        "resolved_model": resolved_model,
        "venv_creator": (
            resolved_venv_creator.to_dict() if resolved_venv_creator is not None else None
        ),
        "reuse_existing_venv": reusable_existing_venv,
        "out_free_bytes": out_free,
        "data_free_bytes": data_free,
        "runtime_required_bytes": runtime_needed,
        "model_required_bytes": model_needed,
        "blockers": [],
    }
