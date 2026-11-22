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
_SECRET_ENV_SUBSTRINGS = ("API_KEY", "TOKEN", "PASSWORD", "SECRET", "AUTH")
_SAFE_DERIVED_KEYS = (
    "exe",
    "args",
    "cwd",
    "tokenizer",
    "model_name",
    "display_name",
    "port",
    "backend",
    "gpu",
    "gpus_asked",
    "hip_ordinal",
    "layer_split",
    "split_skip_if_fits",
    "lib_dirs",
    "cuda",
    "tool_call_recovery",
    "sampling",
    "vision",
    "env",
)
UPSTREAM_DEFAULT_CONTEXT = 4096


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


def _has_secret_mapping(mapping: dict) -> bool:
    for key, val in mapping.items():
        upper = str(key).upper()
        if any(part in upper for part in _SECRET_ENV_SUBSTRINGS) and str(val).strip():
            return True
    return False


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
    if isinstance(env, dict) and _has_secret_mapping(env):
        raise PreparationError("polystrata_plaintext_credential_rejected")
    for mcp_key in ("mcp_servers", "mcpServers"):
        mcp = payload.get(mcp_key)
        if isinstance(mcp, dict):
            for entry in mcp.values():
                if not isinstance(entry, dict):
                    continue
                for subkey in ("headers", "env"):
                    sub = entry.get(subkey)
                    if isinstance(sub, dict) and _has_secret_mapping(sub):
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
        stem = exe_path.stem.lower()
        if stem in ("strata-poly", "strata-glm") or "engine-poly" in (
            p.lower() for p in exe_path.parts
        ):
            return True
    return False


def _discover_tokenizer_dir(installation: dict, model_input: Path) -> Path | None:
    """Find a model-matched upstream PolyStrata tokenizer directory (containing vocab.json)."""
    sibling = model_input.parent / "tokenizer"
    if (sibling / "vocab.json").is_file():
        return sibling.resolve(strict=True)
    root = Path(installation["working_directory"])
    packs_roots: list[Path] = []
    if installation.get("data_dir"):
        packs_roots.append(Path(installation["data_dir"]) / "packs")
    packs_roots.append(root / "data" / "packs")
    stem_lower = model_input.stem.lower()
    for packs_dir in packs_roots:
        if not packs_dir.is_dir():
            continue
        for child in sorted(packs_dir.iterdir()):
            tok = child / "tokenizer"
            if (tok / "vocab.json").is_file() and child.name.lower() in stem_lower:
                return tok.resolve(strict=True)
    return None


def inspect_polystrata_payload(
    payload: dict,
    config_file: Path,
    bases: tuple[Path, ...],
) -> tuple[Path | None, int | None, str | None, Path | None]:
    """Parse a PolyStrata JSON payload -> (model_asset, context, model_flag, fallback_tokenizer)."""
    check_no_plaintext_credentials(payload)
    ordered_bases: list[Path] = []
    cwd_val = payload.get("cwd")
    if isinstance(cwd_val, str) and cwd_val.strip():
        cwd_path = Path(cwd_val.strip())
        if cwd_path.is_dir():
            ordered_bases.append(cwd_path)
    for item in bases:
        if item not in ordered_bases:
            ordered_bases.append(item)
    base_tuple = tuple(ordered_bases)

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
    served_model: str | None,
    default_asset_dir: Path | None = None,
) -> tuple[Path, Path, dict | None, int, Path | None, str, Path | None, str | None]:
    payload = read_json(config_file, "polystrata_config_invalid")
    if not isinstance(payload, dict):
        raise PreparationError("polystrata_config_invalid")
    bases: list[Path] = [Path(installation["working_directory"]), config_file.parent]
    if installation.get("data_dir"):
        bases.append(Path(installation["data_dir"]))
    base_tuple = tuple(bases)

    model_asset, existing_ctx, model_flag, fallback_tokenizer = inspect_polystrata_payload(
        payload, config_file, base_tuple
    )
    if model_asset is None:
        if default_asset_dir is not None:
            model_asset = default_asset_dir
        elif (config_file.parent / "tokenizer_config.json").is_file() or (
            config_file.parent / "chat_template.jinja"
        ).is_file():
            model_asset = config_file.parent.resolve(strict=True)
        else:
            raise PreparationError("polystrata_config_model_missing")

    if model_asset.is_file() and model_asset.suffix.lower() == ".gguf" and _is_poly_config(payload):
        _check_gguf_arch(model_asset)

    resolved_exe_dir: Path | None = None
    if isinstance(payload.get("exe"), str) and payload["exe"].strip():
        resolved_exe = _resolve_ref(payload["exe"].strip(), base_tuple)
        if resolved_exe is not None:
            resolved_exe_dir = resolved_exe.parent
    explicit_backend = (
        payload["backend"].strip().lower()
        if isinstance(payload.get("backend"), str) and payload["backend"].strip()
        else None
    )

    effective_existing_ctx = existing_ctx if existing_ctx is not None else UPSTREAM_DEFAULT_CONTEXT
    resolved_context = context_size if context_size is not None else effective_existing_ctx
    json_model_name = (
        payload["model_name"].strip()
        if isinstance(payload.get("model_name"), str) and payload["model_name"].strip()
        else None
    )
    resolved_served = served_model or json_model_name or "inferyard-local-model"

    needs_derive = (context_size is not None and existing_ctx != context_size) or (
        served_model is not None and served_model != json_model_name
    )
    derived_payload: dict | None = None
    config_path = config_file
    if needs_derive:
        derived_payload = {k: payload[k] for k in _SAFE_DERIVED_KEYS if k in payload}
        derived_payload["model_name"] = resolved_served
        if isinstance(derived_payload.get("args"), list):
            new_args = [str(x) for x in derived_payload["args"]]
            if model_flag and model_flag.startswith("--"):
                new_args = _set_arg_value(new_args, model_flag, str(model_asset))
            new_args = _set_arg_value(new_args, "--max-context", str(resolved_context))
            derived_payload["args"] = new_args
            if isinstance(derived_payload.get("exe"), str):
                resolved_exe = _resolve_ref(derived_payload["exe"], base_tuple)
                if resolved_exe is not None:
                    derived_payload["exe"] = str(resolved_exe)
        else:
            derived_payload["context"] = resolved_context
            if model_flag and model_flag.startswith("key:"):
                derived_payload[model_flag[4:]] = str(model_asset)
        config_path = out_dir / "strata-candidate.json"
    return (
        model_asset,
        config_path,
        derived_payload,
        resolved_context,
        fallback_tokenizer,
        resolved_served,
        resolved_exe_dir,
        explicit_backend,
    )


def resolve_polystrata_model(
    installation: dict,
    model_input: Path,
    out_dir: Path,
    context_size: int | None,
    *,
    system: str,
    served_model: str | None,
    port: int,
) -> tuple[Path, Path, dict | None, int, Path | None, str, str]:
    """Resolve GGUF, strata-*.json, or pack dir -> (asset, cfg, derived, ctx, tok, served, backend)."""
    if model_input.is_file():
        suffix = model_input.suffix.lower()
        if suffix == ".gguf":
            metadata = _check_gguf_arch(model_input)
            root = Path(installation["working_directory"])
            binaries = find_polystrata_engine_binaries(root, platform_system=system)
            poly_exe = binaries.get("poly")
            if poly_exe is None:
                raise PreparationError("local_engine_polystrata_engine_missing")
            backend = select_polystrata_backend(installation, system, engine_dir=poly_exe.parent)
            resolved_context = context_size or 8192
            display_name = metadata.get("general.name") or model_input.stem
            resolved_served = served_model or display_name
            derived: dict = {
                "exe": str(poly_exe),
                "args": ["--model", str(model_input), "--max-context", str(resolved_context)],
                "cwd": str(root),
                "model_name": resolved_served,
                "port": port,
            }
            tok_dir = _discover_tokenizer_dir(installation, model_input)
            if tok_dir is not None:
                derived["tokenizer"] = str(tok_dir)
            if backend == "hip":
                derived["backend"] = "hip"
            return (
                model_input,
                out_dir / "strata-candidate.json",
                derived,
                resolved_context,
                tok_dir,
                resolved_served,
                backend,
            )
        if suffix == ".json":
            (
                asset,
                cfg_path,
                derived,
                ctx,
                tok,
                resolved_served,
                exe_dir,
                explicit_backend,
            ) = _resolve_existing_json(
                installation, model_input, out_dir, context_size, served_model=served_model
            )
            backend = select_polystrata_backend(
                installation,
                system,
                engine_dir=exe_dir,
                explicit_backend=explicit_backend,
            )
            return asset, cfg_path, derived, ctx, tok, resolved_served, backend
        raise PreparationError("polystrata_model_unsupported")

    if model_input.is_dir():
        matches = sorted(
            p
            for p in model_input.iterdir()
            if p.is_file() and p.name.startswith("strata") and p.suffix == ".json"
        )
        if len(matches) != 1:
            raise PreparationError("polystrata_config_required")
        (
            asset,
            cfg_path,
            derived,
            ctx,
            tok,
            resolved_served,
            exe_dir,
            explicit_backend,
        ) = _resolve_existing_json(
            installation,
            matches[0].resolve(strict=True),
            out_dir,
            context_size,
            served_model=served_model,
            default_asset_dir=model_input,
        )
        backend = select_polystrata_backend(
            installation,
            system,
            engine_dir=exe_dir,
            explicit_backend=explicit_backend,
        )
        return asset, cfg_path, derived, ctx, tok, resolved_served, backend

    raise PreparationError("polystrata_model_unsupported")


def select_polystrata_backend(
    installation: dict,
    system: str,
    *,
    engine_dir: Path | None = None,
    explicit_backend: str | None = None,
) -> str:
    """Pick the backend for a PolyStrata candidate from platform, BUILD.json, or host GPUs."""
    if system == "Darwin":
        return "metal"
    if explicit_backend in ("cuda", "hip", "cpu"):
        return explicit_backend
    root = Path(installation["working_directory"])
    if engine_dir is None:
        binaries = find_polystrata_engine_binaries(root, platform_system=system)
        engine_dir = (
            binaries["poly"].parent
            if "poly" in binaries
            else binaries["strata"].parent
            if "strata" in binaries
            else None
        )
    declared = read_build_backend(root, engine_dir=engine_dir)
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
