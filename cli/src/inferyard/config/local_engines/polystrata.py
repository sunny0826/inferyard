"""PolyStrata candidate model resolution, GGUF sharing, and backend selection."""

from __future__ import annotations

from pathlib import Path

from inferyard.contracts.engine_profiles import POLYSTRATA_SUPPORTED_GGUF_ARCHS
from inferyard.platforms.gguf_metadata import read_metadata
from inferyard.platforms.local_engines.detect_polystrata import (
    find_polystrata_engine_binaries,
    read_build_backend,
)
from inferyard.provisioning.preparation_io import PreparationError, read_json

_LEGACY_MODEL_PATH_KEYS = (
    "gguf",
    "gguf_path",
    "model_path",
    "weights_path",
    "model_dir",
    "pack_dir",
    "weights_dir",
)
_TOKENIZER_PATH_KEYS = ("tokenizer", "tokenizer_path", "tokenizer_dir")
_SECRET_ENV_SUBSTRINGS = ("API_KEY", "TOKEN", "PASSWORD", "SECRET")


def _resolve_ref(raw: str, bases: tuple[Path, ...]) -> Path | None:
    candidate = Path(raw)
    if candidate.is_absolute():
        if candidate.is_symlink():
            raise PreparationError("local_engine_model_symlink_rejected")
        return candidate.resolve(strict=True) if candidate.exists() else None
    for base in bases:
        joined = base / candidate
        if joined.is_symlink():
            raise PreparationError("local_engine_model_symlink_rejected")
        if joined.exists():
            return joined.resolve(strict=True)
    return None


def _check_gguf_arch(gguf_path: Path) -> dict:
    try:
        metadata = read_metadata(gguf_path)
    except (OSError, ValueError, UnicodeError) as exc:
        raise PreparationError("invalid_input") from exc
    arch = metadata.get("general.architecture")
    if not isinstance(arch, str) or arch not in POLYSTRATA_SUPPORTED_GGUF_ARCHS:
        raise PreparationError("polystrata_gguf_architecture_unsupported")
    return metadata


def check_no_plaintext_credentials(payload: dict) -> None:
    """Reject plaintext credentials in PolyStrata JSON configs (use env vars instead)."""
    api_key = payload.get("api_key")
    if isinstance(api_key, str) and api_key.strip():
        raise PreparationError("polystrata_plaintext_credential_rejected")
    if api_key not in (None, ""):
        raise PreparationError("polystrata_plaintext_credential_rejected")
    args = payload.get("args")
    if isinstance(args, list):
        for item in args:
            if isinstance(item, str) and item.split("=", 1)[0].lower() in (
                "--api-key",
                "--api-key-file",
                "--token",
                "--hf-token",
            ):
                raise PreparationError("polystrata_plaintext_credential_rejected")
    env = payload.get("env")
    if isinstance(env, dict):
        for key, val in env.items():
            upper = str(key).upper()
            if any(part in upper for part in _SECRET_ENV_SUBSTRINGS) and str(val).strip():
                raise PreparationError("polystrata_plaintext_credential_rejected")


def _extract_arg_value(args: list, flag: str) -> tuple[int | None, str | None]:
    for idx, item in enumerate(args):
        if not isinstance(item, str):
            continue
        if item == flag and idx + 1 < len(args) and isinstance(args[idx + 1], str):
            return idx, args[idx + 1]
        if item.startswith(flag + "="):
            return idx, item.split("=", 1)[1]
    return None, None


def _set_arg_value(args: list[str], flag: str, value: str) -> list[str]:
    updated = list(args)
    for idx, item in enumerate(updated):
        if item == flag and idx + 1 < len(updated):
            updated[idx + 1] = value
            return updated
        if item.startswith(flag + "="):
            updated[idx] = f"{flag}={value}"
            return updated
    updated.extend([flag, value])
    return updated


def _is_poly_config(payload: dict) -> bool:
    if payload.get("engine") == "strata-poly":
        return True
    exe = payload.get("exe")
    if isinstance(exe, str) and exe.strip():
        exe_path = Path(exe.strip())
        if "poly" in exe_path.stem.lower() or "engine-poly" in (p.lower() for p in exe_path.parts):
            return True
    return False


def inspect_polystrata_payload(
    payload: dict,
    config_file: Path,
    bases: tuple[Path, ...],
) -> tuple[Path | None, int | None, str | None, Path | None]:
    """Parse a PolyStrata JSON payload -> (model_asset, context, model_flag, fallback_tokenizer)."""
    check_no_plaintext_credentials(payload)
    extra_bases = list(bases)
    cwd_val = payload.get("cwd")
    if isinstance(cwd_val, str) and cwd_val.strip():
        cwd_path = Path(cwd_val.strip())
        if cwd_path.is_dir() and cwd_path not in extra_bases:
            extra_bases.insert(0, cwd_path)
    base_tuple = tuple(extra_bases)

    args = payload.get("args") if isinstance(payload.get("args"), list) else []
    model_asset: Path | None = None
    model_flag: str | None = None
    for flag in ("--model", "--native", "--pack"):
        _, raw_val = _extract_arg_value(args, flag)
        if raw_val and raw_val.strip():
            resolved = _resolve_ref(raw_val.strip(), base_tuple)
            if resolved is not None:
                model_asset = resolved
                model_flag = flag
                break

    if model_asset is None:
        for key in _LEGACY_MODEL_PATH_KEYS:
            raw = payload.get(key)
            if isinstance(raw, str) and raw.strip():
                resolved = _resolve_ref(raw.strip(), base_tuple)
                if resolved is not None:
                    model_asset = resolved
                    model_flag = f"key:{key}"
                    break

    _, raw_ctx = _extract_arg_value(args, "--max-context")
    existing_ctx: int | None = None
    if raw_ctx is not None:
        try:
            parsed_ctx = int(raw_ctx)
            if parsed_ctx > 0:
                existing_ctx = parsed_ctx
        except ValueError:
            existing_ctx = None
    if existing_ctx is None:
        cfg_ctx = payload.get("context")
        if type(cfg_ctx) is int and cfg_ctx > 0:
            existing_ctx = cfg_ctx

    fallback_tokenizer: Path | None = None
    for key in _TOKENIZER_PATH_KEYS:
        raw = payload.get(key)
        if isinstance(raw, str) and raw.strip():
            resolved = _resolve_ref(raw.strip(), base_tuple)
            if resolved is not None:
                fallback_tokenizer = resolved
                break
    return model_asset, existing_ctx, model_flag, fallback_tokenizer


def _resolve_existing_json(
    installation: dict,
    config_file: Path,
    out_dir: Path,
    context_size: int | None,
    *,
    default_asset_dir: Path | None = None,
) -> tuple[Path, Path, dict | None, int, Path | None]:
    payload = read_json(config_file, "polystrata_config_invalid")
    if not isinstance(payload, dict):
        raise PreparationError("polystrata_config_invalid")
    bases: list[Path] = [config_file.parent]
    if installation.get("data_dir"):
        bases.append(Path(installation["data_dir"]))
    bases.append(Path(installation["working_directory"]))
    base_tuple = tuple(bases)

    model_asset, existing_ctx, model_flag, fallback_tokenizer = inspect_polystrata_payload(
        payload, config_file, base_tuple
    )
    if model_asset is None:
        if default_asset_dir is not None:
            model_asset = default_asset_dir
        elif (config_file.parent / "tokenizer_config.json").is_file():
            model_asset = config_file.parent.resolve(strict=True)
        else:
            raise PreparationError("polystrata_config_model_missing")

    if model_asset.is_file() and model_asset.suffix.lower() == ".gguf" and _is_poly_config(payload):
        _check_gguf_arch(model_asset)

    resolved_context = context_size or existing_ctx or 8192

    derived_payload: dict | None = None
    config_path = config_file
    if context_size is not None and existing_ctx != context_size:
        derived_payload = {k: v for k, v in payload.items() if k != "api_key"}
        if isinstance(derived_payload.get("args"), list):
            new_args = [str(x) for x in derived_payload["args"]]
            if model_flag and model_flag.startswith("--"):
                new_args = _set_arg_value(new_args, model_flag, str(model_asset))
            new_args = _set_arg_value(new_args, "--max-context", str(context_size))
            derived_payload["args"] = new_args
            if isinstance(derived_payload.get("exe"), str):
                resolved_exe = _resolve_ref(derived_payload["exe"], base_tuple)
                if resolved_exe is not None:
                    derived_payload["exe"] = str(resolved_exe)
        else:
            derived_payload["context"] = context_size
            if model_flag and model_flag.startswith("key:"):
                derived_payload[model_flag[4:]] = str(model_asset)
        config_path = out_dir / "strata-candidate.json"
    return model_asset, config_path, derived_payload, resolved_context, fallback_tokenizer


def resolve_polystrata_model(
    installation: dict,
    model_input: Path,
    out_dir: Path,
    context_size: int | None,
    *,
    backend: str,
    port: int,
) -> tuple[Path, Path, dict | None, int, Path | None]:
    """Resolve a GGUF, strata-*.json, or pack dir into (asset, config_path, derived, ctx, tok)."""
    if model_input.is_file():
        suffix = model_input.suffix.lower()
        if suffix == ".gguf":
            metadata = _check_gguf_arch(model_input)
            root = Path(installation["working_directory"])
            binaries = find_polystrata_engine_binaries(root)
            poly_exe = binaries.get("poly") or binaries.get("strata")
            if poly_exe is None:
                raise PreparationError("local_engine_polystrata_engine_missing")
            resolved_context = context_size or 8192
            display_name = metadata.get("general.name") or model_input.stem
            derived: dict = {
                "exe": str(poly_exe),
                "args": ["--model", str(model_input), "--max-context", str(resolved_context)],
                "cwd": str(root),
                "model_name": display_name,
                "port": port,
            }
            if backend == "hip":
                derived["backend"] = "hip"
            return (
                model_input,
                out_dir / "strata-candidate.json",
                derived,
                resolved_context,
                None,
            )
        if suffix == ".json":
            return _resolve_existing_json(installation, model_input, out_dir, context_size)
        raise PreparationError("polystrata_model_unsupported")

    if model_input.is_dir():
        matches = sorted(
            p
            for p in model_input.iterdir()
            if p.is_file() and p.name.startswith("strata") and p.suffix == ".json"
        )
        if len(matches) != 1:
            raise PreparationError("polystrata_config_required")
        return _resolve_existing_json(
            installation,
            matches[0].resolve(strict=True),
            out_dir,
            context_size,
            default_asset_dir=model_input,
        )

    raise PreparationError("polystrata_model_unsupported")


def select_polystrata_backend(installation: dict, system: str) -> str:
    """Pick the backend for a PolyStrata candidate from platform, BUILD.json, or host GPUs."""
    if system == "Darwin":
        return "metal"
    root = Path(installation["working_directory"])
    binaries = find_polystrata_engine_binaries(root, platform_system=system)
    preferred_dir = (
        binaries["poly"].parent
        if "poly" in binaries
        else binaries["strata"].parent
        if "strata" in binaries
        else None
    )
    declared = read_build_backend(root, engine_dir=preferred_dir)
    if declared in ("cuda", "hip", "cpu"):
        return declared
    from inferyard.platforms.device_gpu import detect_gpus

    gpu = detect_gpus(system)
    if gpu.get("status") == "observed":
        vendors = {
            str(dev.get("vendor") or "").lower()
            for dev in gpu.get("devices") or ()
            if isinstance(dev, dict)
        }
        if "nvidia" in vendors:
            return "cuda"
        if "amd" in vendors:
            return "hip"
    return "cpu"
