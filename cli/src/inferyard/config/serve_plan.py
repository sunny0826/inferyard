"""Lightweight standalone serve plan builder without hashing or benchmark recipes."""

from __future__ import annotations

import hashlib
import json
import os
import platform
import re
import tempfile
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.config.local_engines.launch import startup
from inferyard.config.local_engines.polystrata import (
    inspect_polystrata_payload,
    resolve_polystrata_model,
)
from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES, LOCAL_ADAPTER
from inferyard.contracts.validation import json_bytes, strict_json_loads, validate_document
from inferyard.errors import PreflightError
from inferyard.platforms.gguf_metadata import read_metadata
from inferyard.platforms.local_engines.installation import read_installation
from inferyard.provisioning.preparation_io import PreparationError, linked, read_json
from inferyard.provisioning.runtime_profiles import exact_value, load_profile, output_files
from inferyard.provisioning.runtime_verification import inside_file

_RUN_ID_INVALID = re.compile(r"[^A-Za-z0-9_.-]+")
_UNSUPPORTED_SERVE_FAMILIES = ("lmstudio", "mlx-lm")
_RECEIPT_KEYS = {
    "kind",
    "schema_version",
    "profile",
    "profile_sha256",
    "platform",
    "architecture",
    "mode",
    "engine_release",
    "archives",
    "engine_binary",
    "runtime_library_manifest",
    "engine_sha256",
    "libraries",
    "version",
    "model_requests_sent",
    "ready_to_run",
}


def _write_atomic_json(path: Path, payload: dict) -> None:
    data = json.dumps(payload, ensure_ascii=False, allow_nan=False, indent=2) + "\n"
    fd, tmp_name = tempfile.mkstemp(dir=path.parent, prefix=f".{path.name}.", suffix=".tmp")
    tmp_path = Path(tmp_name)
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
            stream.write(data)
            stream.flush()
        os.chmod(tmp_path, 0o600)
        os.replace(tmp_path, path)
    except OSError:
        tmp_path.unlink(missing_ok=True)
        raise


def _check_gguf_context(model: Path, context_size: int) -> dict:
    try:
        metadata = read_metadata(model)
    except (OSError, ValueError, UnicodeError) as exc:
        raise PreparationError("invalid_or_unreadable_gguf") from exc
    arch = metadata.get("general.architecture")
    maximum = metadata.get(f"{arch}.context_length") if isinstance(arch, str) else None
    if type(maximum) is not int or maximum <= 0 or context_size > maximum:
        raise PreparationError("model_context_unsupported")
    return metadata


def _directory_context_limit(model_dir: Path) -> int | None:
    config_path = model_dir / "config.json"
    if not config_path.is_file():
        return None
    try:
        raw = strict_json_loads(config_path.read_text(encoding="utf-8"))
    except (OSError, ValueError, UnicodeError):
        return None
    if not isinstance(raw, dict):
        return None
    candidates = [raw.get("max_position_embeddings")]
    text_config = raw.get("text_config")
    if isinstance(text_config, dict):
        candidates.append(text_config.get("max_position_embeddings"))
    for value in candidates:
        if type(value) is int and value > 0:
            return value
    return None


def _check_directory_context(model_dir: Path, context_size: int) -> None:
    maximum = _directory_context_limit(model_dir)
    if maximum is None or context_size > maximum:
        raise PreparationError("model_context_unsupported")


def _polystrata_declared_context(installation: dict, model_input: Path) -> int | None:
    config_file: Path | None = None
    if model_input.is_file() and model_input.suffix.lower() == ".json":
        config_file = model_input
    elif model_input.is_dir():
        matches = sorted(
            p
            for p in model_input.iterdir()
            if p.is_file() and p.name.startswith("strata") and p.suffix == ".json"
        )
        if len(matches) == 1:
            config_file = matches[0].resolve(strict=True)
    if config_file is None:
        return None
    payload = read_json(config_file, "polystrata_config_invalid")
    if not isinstance(payload, dict):
        return None
    bases: list[Path] = [Path(installation["working_directory"]), config_file.parent]
    if installation.get("data_dir"):
        bases.append(Path(installation["data_dir"]))
    _, existing_ctx, _, _ = inspect_polystrata_payload(payload, config_file, tuple(bases))
    return existing_ctx


def _prepare_output_dir(out_arg: Path) -> Path:
    if out_arg.is_symlink():
        raise PreparationError("invalid_input")
    if out_arg.exists() and not out_arg.is_dir():
        raise PreparationError("invalid_input")
    out_arg.mkdir(parents=True, exist_ok=True)
    return out_arg.resolve(strict=True)


def _resolve_receipt_engine(receipt_path: Path) -> tuple[Path, str]:
    receipt_abs = receipt_path.absolute()
    if linked(receipt_abs) or not receipt_abs.is_file():
        raise PreparationError("runtime_receipt_invalid")
    receipt = read_json(receipt_abs, "runtime_receipt_invalid")
    if (
        type(receipt) is not dict
        or set(receipt) != _RECEIPT_KEYS
        or type(receipt.get("profile")) is not str
    ):
        raise PreparationError("runtime_receipt_invalid")
    profile, digest = load_profile(receipt["profile"])
    constant = dict(
        kind="community_runtime_receipt.v1",
        schema_version=SCHEMA_VERSION,
        profile_sha256=digest,
        model_requests_sent=0,
        ready_to_run=False,
        engine_sha256=profile["libraries"]["llama-server.exe"],
    )
    constant.update(
        {
            key: profile[key]
            for key in (
                "profile",
                "platform",
                "architecture",
                "mode",
                "engine_release",
                "archives",
                "engine_binary",
                "runtime_library_manifest",
                "libraries",
            )
        }
    )
    if any(not exact_value(receipt[key], value) for key, value in constant.items()):
        raise PreparationError("runtime_profile_mismatch")
    version = receipt["version"]
    if (
        type(version) is not dict
        or set(version) != {"argv", "returncode", "stdout", "stderr"}
        or version["argv"] != ["--version"]
        or type(version["returncode"]) is not int
        or version["returncode"] != 0
        or type(version["stdout"]) is not str
        or type(version["stderr"]) is not str
    ):
        raise PreparationError("runtime_receipt_invalid")
    root = receipt_abs.parent
    _, manifest_rel, _, expected = output_files(profile["members"])
    try:
        manifest_path = inside_file(root, manifest_rel)
    except OSError as exc:
        raise PreparationError("runtime_asset_mismatch") from exc
    if read_json(manifest_path, "runtime_asset_mismatch") != profile["libraries"]:
        raise PreparationError("runtime_asset_mismatch")
    for relative, member in expected.items():
        try:
            item = inside_file(root, relative)
            size = item.stat().st_size
        except OSError as exc:
            raise PreparationError("runtime_asset_mismatch") from exc
        if size != member["bytes"]:
            raise PreparationError("runtime_asset_mismatch")
    try:
        engine_path = inside_file(root, profile["engine_binary"]).resolve(strict=True)
    except OSError as exc:
        raise PreparationError("runtime_asset_mismatch") from exc
    return engine_path, profile["mode"]


def _sanitize_run_id(name: str) -> str:
    cleaned = _RUN_ID_INVALID.sub("-", name).strip(".-")
    return f"inferyard-serve-{cleaned or 'plan'}"[:128]


def _prepare_community(request, model: Path, out: Path, port: int) -> dict:
    if not model.is_file() or model.suffix.lower() != ".gguf":
        raise PreparationError("invalid_input")
    metadata = _check_gguf_context(model, request.context_size)
    if request.runtime_receipt is not None:
        executable, mode = _resolve_receipt_engine(request.runtime_receipt)
        ngl = "999" if mode == "cuda" else "0"
    else:
        try:
            executable = request.engine_path.resolve(strict=True)
        except OSError as exc:
            raise PreparationError("invalid_input") from exc
        if not executable.is_file():
            raise PreparationError("invalid_input")
        ngl = "99" if platform.system() == "Darwin" else "0"
    served = metadata.get("general.name") or model.stem
    argv = [
        str(executable),
        "-m",
        str(model),
        "-ngl",
        ngl,
        "-c",
        str(request.context_size),
        "--host",
        "127.0.0.1",
        "--port",
        str(port),
        "--jinja",
        "--alias",
        served,
    ]
    return {
        "schema_version": SCHEMA_VERSION,
        "argv": argv,
        "environment": {},
        "working_directory": str(executable.parent),
        "endpoint": f"http://127.0.0.1:{port}",
        "model_path": str(model),
        "model_kind": "gguf",
        "context_size": request.context_size,
        "family": "llama-cpp",
        "adapter": "prism_llama_server_v1",
        "local": False,
    }


def _prepare_installation(request, model: Path, out: Path, port: int) -> dict:
    inst_path = request.installation
    if inst_path.is_dir():
        inst_path = inst_path / "installation.json"
    try:
        installation = read_installation(inst_path)
    except PreflightError as exc:
        raise PreparationError(str(exc)) from exc
    family = installation["family"]
    if family in _UNSUPPORTED_SERVE_FAMILIES:
        raise PreparationError("serve_family_unsupported")

    polystrata_config_path = None
    resolved_model = model
    if family == "polystrata":
        system = platform.system()
        (
            asset,
            polystrata_config_path,
            derived_polystrata_config,
            _,
            _,
            served,
            _,
        ) = resolve_polystrata_model(
            installation,
            model,
            out,
            request.context_size,
            system=system,
            served_model=None,
            port=port,
        )
        declared_limit = _polystrata_declared_context(installation, model)
        if asset.is_file() and asset.suffix.lower() == ".gguf":
            _check_gguf_context(asset, request.context_size)
            if declared_limit is not None and request.context_size > declared_limit:
                raise PreparationError("model_context_unsupported")
        else:
            limits = [v for v in (declared_limit,) if v is not None]
            if asset.is_dir():
                dir_limit = _directory_context_limit(asset)
                if dir_limit is not None:
                    limits.append(dir_limit)
            if limits and request.context_size > min(limits):
                raise PreparationError("model_context_unsupported")
        model_kind = "gguf" if model.is_file() and model.suffix.lower() == ".gguf" else "strata_pack"
        resolved_model = asset
        if derived_polystrata_config is not None:
            digest = hashlib.sha256(json_bytes(derived_polystrata_config)).hexdigest()[:12]
            polystrata_config_path = out / f"strata-serve-{digest}.json"
            _write_atomic_json(polystrata_config_path, derived_polystrata_config)
    elif family == "llama-cpp":
        if not model.is_file() or model.suffix.lower() != ".gguf":
            raise PreparationError("invalid_input")
        metadata = _check_gguf_context(model, request.context_size)
        served = metadata.get("general.name") or model.stem
        model_kind = "gguf"
    elif family in DIRECTORY_FAMILIES:
        if not model.is_dir():
            raise PreparationError("invalid_input")
        _check_directory_context(model, request.context_size)
        served = model.name
        model_kind = "directory"
    else:
        raise PreparationError("serve_family_unsupported")

    args, cwd = startup(
        installation,
        resolved_model,
        served,
        request.context_size,
        port,
        polystrata_config=polystrata_config_path,
        standalone=True,
    )
    environment = dict(installation["environment"])
    working_directory = cwd
    if family == "sglang":
        work_dir = out / "sglang-work"
        metrics_dir = work_dir / "metrics"
        work_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        metrics_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        working_directory = str(work_dir)
        environment["PROMETHEUS_MULTIPROC_DIR"] = str(metrics_dir)
        environment["SGLANG_RUN_ID"] = _sanitize_run_id(out.name)

    return {
        "schema_version": SCHEMA_VERSION,
        "argv": [installation["executable"], *args],
        "environment": environment,
        "working_directory": working_directory,
        "endpoint": f"http://127.0.0.1:{port}",
        "model_path": str(model),
        "model_kind": model_kind,
        "context_size": request.context_size,
        "family": family,
        "adapter": LOCAL_ADAPTER,
        "local": True,
    }


def prepare(request) -> dict:
    if type(request.context_size) is not int or request.context_size <= 0:
        raise PreparationError("invalid_input")
    port = 48857 if request.port is None else request.port
    if type(port) is not int or not 1 <= port <= 65535:
        raise PreparationError("invalid_input")
    try:
        model = request.model_path.resolve(strict=True)
    except OSError as exc:
        raise PreparationError("invalid_input") from exc
    out = _prepare_output_dir(request.out)
    if request.installation is not None:
        plan = _prepare_installation(request, model, out, port)
    else:
        plan = _prepare_community(request, model, out, port)
    validate_document("serve_plan", plan)
    plan_path = out / "serve-plan.json"
    _write_atomic_json(plan_path, plan)
    return {"plan_path": str(plan_path), **plan}
