"""Managed PolyStrata setup orchestration and verified installation generation."""

from __future__ import annotations

from collections.abc import Callable
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

from inferyard.config.local_engines.polystrata import (
    _check_gguf_arch,
    _is_poly_config,
    inspect_polystrata_payload,
)
from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import PreflightError
from inferyard.platforms.local_engines.detect import build_installation, write_installation
from inferyard.platforms.local_engines.detect_polystrata import (
    pick_venv_python,
    polystrata_model_configs,
    read_build_backend,
    read_build_version,
    validate_polystrata_layout,
)
from inferyard.platforms.platform_io import sync_directory
from inferyard.provisioning.acquire_errors import classify_os_error, failure_details
from inferyard.provisioning.engine_source_download import validate_sha256
from inferyard.provisioning.http_client import validate_proxy_origin
from inferyard.provisioning.polystrata_archive import (
    DEFAULT_POLYSTRATA_SOURCE_URL,
    INSTALLATION_FILE_NAME,
    POLYSTRATA_PINNED_TAG,
    SETUP_LOG_NAME,
    acquire_runtime_tree,
    resolve_polystrata_source,
)
from inferyard.provisioning.polystrata_preflight import run_polystrata_preflight
from inferyard.provisioning.polystrata_python import (
    VenvCreator,
    build_venv_command,
    is_supported_python_version,
    probe_python_version,
    resolve_polystrata_venv_creator,
)
from inferyard.provisioning.polystrata_runner import run_setup_process
from inferyard.provisioning.preparation_io import PreparationError, linked, new_path
from inferyard.provisioning.transfer_progress import ProgressEmitter

__all__ = [
    "DEFAULT_POLYSTRATA_SOURCE_URL",
    "INSTALLATION_FILE_NAME",
    "POLYSTRATA_PINNED_TAG",
    "SETUP_LOG_NAME",
    "prepare_polystrata",
]


def _ensure_polystrata_venv(
    out: Path,
    *,
    system: str,
    timeout_seconds: float,
    venv_creator: Callable[[Path], Path] | None = None,
    venv_python: str | None = None,
    resolved_creator: VenvCreator | None = None,
    reuse_existing_venv: bool | None = None,
    proxy_origin: str | None = None,
    env: dict[str, str] | None = None,
    linux_venv_check: Callable[[str], bool] | None = None,
) -> Path:
    venv_dir = out / ".venv"
    if os.path.lexists(venv_dir) and linked(venv_dir):
        raise PreparationError("polystrata_venv_invalid")
    if venv_creator is not None:
        if (venv_dir / "pyvenv.cfg").is_file():
            existing = pick_venv_python(venv_dir, platform_system=system)
            if existing is not None:
                return existing
        return venv_creator(out)
    if (venv_dir / "pyvenv.cfg").is_file():
        existing = pick_venv_python(venv_dir, platform_system=system)
        can_reuse = (
            reuse_existing_venv
            if reuse_existing_venv is not None
            else (
                venv_python is None
                and existing is not None
                and is_supported_python_version(probe_python_version(str(existing)))
            )
        )
        if can_reuse and existing is not None:
            return existing
    if venv_dir.exists():
        shutil.rmtree(venv_dir, ignore_errors=True)
    creator = resolved_creator
    if creator is None:
        creator, blockers = resolve_polystrata_venv_creator(
            system=system,
            venv_python=venv_python,
            env=env,
            venv_check=linux_venv_check,
        )
        if creator is None:
            primary = blockers[0]["code"] if blockers else "polystrata_python_unsupported"
            raise PreparationError(
                primary,
                code=2,
                details={**failure_details("setup_engine", primary), "blockers": blockers},
            )
    cmd = build_venv_command(creator, venv_dir)
    run_env = dict(os.environ)
    if env is not None:
        run_env.update(env)
    if proxy_origin:
        validated = validate_proxy_origin(proxy_origin)
        assert validated is not None
        for key in ("HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"):
            run_env[key] = validated
    try:
        res = subprocess.run(
            cmd,
            env=run_env,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=min(float(timeout_seconds), 300.0),
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        cause = classify_os_error(exc) if isinstance(exc, OSError) else "timeout"
        raise PreparationError(
            "polystrata_venv_failed",
            code=2,
            details=failure_details("setup_engine", cause),
        ) from None
    if res.returncode != 0:
        raise PreparationError(
            "polystrata_venv_failed",
            code=2,
            details=failure_details("setup_engine", "polystrata_venv_failed"),
        )
    created = pick_venv_python(venv_dir, platform_system=system)
    if created is None:
        raise PreparationError("polystrata_venv_missing")
    return created


def _is_usable_exe(path: Path, system: str) -> bool:
    if not path.is_file():
        return False
    if system == "Windows":
        return True
    return os.access(path, os.X_OK)


def _valid_model_configs(out: Path, data_dir: Path, *, system: str) -> list[str]:
    candidates = polystrata_model_configs(out, data_dir)
    valid: list[tuple[float, str]] = []
    for raw_path in candidates:
        path = Path(raw_path)
        try:
            data = strict_json_loads(path.read_text(encoding="utf-8-sig"))
        except (OSError, ValueError, UnicodeError):
            continue
        if not isinstance(data, dict):
            continue
        exe_raw = data.get("exe")
        args_raw = data.get("args")
        if (
            not isinstance(exe_raw, str)
            or not exe_raw.strip()
            or not isinstance(args_raw, list)
            or not all(isinstance(item, str) for item in args_raw)
        ):
            continue
        cwd_raw = data.get("cwd")
        base_dir = Path(cwd_raw) if isinstance(cwd_raw, str) and cwd_raw.strip() else out
        if not base_dir.is_absolute():
            base_dir = out / base_dir
        exe_path = Path(exe_raw)
        if not exe_path.is_absolute():
            exe_path = base_dir / exe_path
        if not _is_usable_exe(exe_path, system):
            continue
        model_asset, _, _, _ = inspect_polystrata_payload(data, path, (out, path.parent, data_dir))
        if model_asset is None:
            continue
        if (
            model_asset.is_file()
            and model_asset.suffix.lower() == ".gguf"
            and _is_poly_config(data)
        ):
            try:
                _check_gguf_arch(model_asset)
            except PreparationError:
                continue
        try:
            mtime = path.stat().st_mtime
        except OSError:
            mtime = 0.0
        valid.append((mtime, raw_path))
    valid.sort(key=lambda item: (-item[0], item[1]))
    return [path for _, path in valid]


def _write_installation_file(target: Path, payload: dict, *, resume: bool) -> str:
    if not resume or not os.path.lexists(target):
        try:
            return write_installation(target, payload)
        except FileExistsError:
            raise PreparationError("output_exists") from None
    if linked(target) or not target.is_file():
        raise PreparationError("output_exists")
    encoded = (json.dumps(payload, ensure_ascii=False, indent=2, allow_nan=False) + "\n").encode(
        "utf-8"
    )
    fd, tmp_name = tempfile.mkstemp(dir=target.parent, prefix=".installation-", suffix=".tmp")
    tmp_path = Path(tmp_name)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(encoded)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(tmp_path, 0o600)
        os.replace(tmp_path, target)
        sync_directory(target.parent)
    finally:
        try:
            tmp_path.unlink(missing_ok=True)
        except OSError:
            pass
    return str(target.resolve(strict=True))


def prepare_polystrata(
    out: str | Path,
    *,
    data_dir: str | Path | None = None,
    family: str | None = None,
    model: str | None = None,
    context: int | None = None,
    backend: str | None = None,
    model_source: str | None = None,
    gguf_path: str | Path | None = None,
    source: str | None = None,
    expected_sha256: str | None = None,
    proxy_origin: str | None = None,
    resume: bool = False,
    progress_stream: bool = False,
    timeout_seconds: float = 3600,
    client=None,
    emitter: ProgressEmitter | None = None,
    platform_info: tuple[str, str] | None = None,
    macos_version: str | None = None,
    xcode_clt_check: Callable[[], bool] | None = None,
    linux_venv_check: Callable[[str], bool] | None = None,
    gpu_probe: Callable[[str], dict] | None = None,
    build_tools_check: Callable[[bool], list[str]] | None = None,
    disk_usage_fn=shutil.disk_usage,
    venv_creator: Callable[[Path], Path] | None = None,
    venv_python: str | None = None,
    env: dict[str, str] | None = None,
) -> dict:
    """Run managed 4-stage PolyStrata setup: preflight -> runtime -> setup.py -> verify."""
    expected_sha256 = validate_sha256(expected_sha256)
    remote_source, local_archive, kind = resolve_polystrata_source(source)
    gguf_resolved = Path(gguf_path).expanduser().absolute() if gguf_path is not None else None

    try:
        if resume:
            candidate = Path(out).absolute()
            parent = candidate.parent.resolve(strict=True)
            if not parent.is_dir():
                raise NotADirectoryError(parent)
            out_path = parent / candidate.name
        else:
            inputs = (gguf_resolved,) if gguf_resolved and gguf_resolved.exists() else ()
            out_path = new_path(out, inputs=inputs)
    except OSError as exc:
        raise PreparationError(
            "io_error",
            4,
            details=failure_details("preflight", classify_os_error(exc)),
        ) from None

    data_path = (
        Path(data_dir).expanduser().absolute()
        if data_dir is not None
        else out_path.parent / "PolyStrata-data"
    )
    progress = emitter if emitter is not None else ProgressEmitter(enabled=progress_stream)

    # Stage 1: read-only preflight
    progress.update(
        "preflight", 0, None, step=1, step_total=4, detail="checking prerequisites", force=True
    )
    preflight = run_polystrata_preflight(
        out_path,
        data_path,
        family=family,
        model=model,
        context=context,
        backend=backend,
        model_source=model_source,
        gguf_path=gguf_resolved,
        proxy_origin=proxy_origin,
        timeout_seconds=timeout_seconds,
        resume=resume,
        platform_info=platform_info,
        macos_version=macos_version,
        xcode_clt_check=xcode_clt_check,
        linux_venv_check=linux_venv_check,
        gpu_probe=gpu_probe,
        build_tools_check=build_tools_check,
        disk_usage_fn=disk_usage_fn,
        venv_python=venv_python,
        env=env,
    )
    system = preflight["platform"]
    effective_family = preflight.get("resolved_family", family)
    effective_model = preflight.get("resolved_model", model)

    # Stage 2: acquire runtime archive into --out
    reused_runtime = acquire_runtime_tree(
        out_path,
        remote_source=remote_source,
        local_archive=local_archive,
        kind=kind,
        expected_sha256=expected_sha256,
        resume=resume,
        timeout_seconds=timeout_seconds,
        proxy_origin=proxy_origin,
        client=client,
        emitter=progress,
    )
    data_path.mkdir(parents=True, exist_ok=True)

    # Stage 3: venv + setup.py --yes --no-start
    resolved_creator = (
        VenvCreator.from_dict(preflight["venv_creator"])
        if isinstance(preflight.get("venv_creator"), dict)
        else None
    )
    venv_py = _ensure_polystrata_venv(
        out_path,
        system=system,
        timeout_seconds=timeout_seconds,
        venv_creator=venv_creator,
        venv_python=venv_python,
        resolved_creator=resolved_creator,
        reuse_existing_venv=bool(preflight.get("reuse_existing_venv")),
        proxy_origin=proxy_origin,
        env=env,
        linux_venv_check=linux_venv_check,
    )
    log_path = run_setup_process(
        out_path,
        venv_py,
        data_path,
        system=system,
        family=effective_family,
        model=effective_model,
        context=context,
        backend=backend,
        model_source=model_source,
        gguf_path=gguf_resolved,
        proxy_origin=proxy_origin,
        timeout_seconds=timeout_seconds,
        emitter=progress,
    )

    # Stage 4: verify .venv, native engine artifacts, strata-*.json, and write installation.json
    progress.update(
        "verifying",
        0,
        None,
        step=4,
        step_total=4,
        detail="verifying PolyStrata installation",
        force=True,
    )
    verified_py = pick_venv_python(out_path / ".venv", platform_system=system)
    if verified_py is None:
        raise PreparationError(
            "polystrata_venv_missing",
            code=2,
            details={
                **failure_details("verifying", "polystrata_venv_missing"),
                "log_path": str(log_path),
            },
        )
    try:
        validate_polystrata_layout(out_path, verified_py, platform_system=system)
    except PreflightError as exc:
        reason = str(exc)
        raise PreparationError(
            reason,
            code=2,
            details={**failure_details("verifying", reason), "log_path": str(log_path)},
        ) from None

    model_configs = _valid_model_configs(out_path, data_path, system=system)
    if not model_configs:
        raise PreparationError(
            "polystrata_config_missing",
            code=2,
            details={
                **failure_details("verifying", "polystrata_config_missing"),
                "log_path": str(log_path),
            },
        )

    try:
        installation = build_installation(
            "polystrata",
            verified_py,
            working_directory=out_path,
            data_dir=data_path if data_path.is_dir() else None,
        )
    except PreflightError as exc:
        reason = str(exc)
        raise PreparationError(
            reason,
            code=2,
            details={**failure_details("verifying", reason), "log_path": str(log_path)},
        ) from None

    installation_path = _write_installation_file(
        out_path / INSTALLATION_FILE_NAME, installation, resume=resume
    )
    progress.update(
        "verifying",
        0,
        None,
        step=4,
        step_total=4,
        percent=100.0,
        detail="PolyStrata installation verified",
        force=True,
    )

    return {
        "out": str(out_path),
        "data_dir": str(data_path),
        "installation_path": installation_path,
        "installation": installation,
        "model_config_path": model_configs[0],
        "model_configs": model_configs,
        "log_path": str(log_path),
        "version": read_build_version(out_path),
        "backend": read_build_backend(out_path),
        "resumed": bool(resume and reused_runtime),
        "ready_to_run": True,
        "model_requests_sent": 0,
    }
