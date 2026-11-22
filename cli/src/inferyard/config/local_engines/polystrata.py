"""PolyStrata candidate model resolution, GGUF sharing, and backend selection."""

from __future__ import annotations

from pathlib import Path

from inferyard.contracts.engine_profiles import POLYSTRATA_SUPPORTED_GGUF_ARCHS
from inferyard.platforms.gguf_metadata import read_metadata
from inferyard.platforms.local_engines.detect_polystrata import read_build_backend
from inferyard.provisioning.preparation_io import PreparationError, read_json

_MODEL_PATH_KEYS = (
    "gguf",
    "gguf_path",
    "model_path",
    "weights_path",
    "model_dir",
    "pack_dir",
    "weights_dir",
)
_TOKENIZER_PATH_KEYS = ("tokenizer", "tokenizer_path", "tokenizer_dir")


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

    asset_key: str | None = None
    model_asset: Path | None = None
    for key in _MODEL_PATH_KEYS:
        raw = payload.get(key)
        if isinstance(raw, str) and raw.strip():
            resolved = _resolve_ref(raw.strip(), base_tuple)
            if resolved is not None:
                asset_key = key
                model_asset = resolved
                break
    if model_asset is None:
        if default_asset_dir is not None:
            model_asset = default_asset_dir
        elif (config_file.parent / "tokenizer_config.json").is_file():
            model_asset = config_file.parent.resolve(strict=True)
        else:
            raise PreparationError("polystrata_config_model_missing")

    if (
        model_asset.is_file()
        and model_asset.suffix.lower() == ".gguf"
        and payload.get("engine") == "strata-poly"
    ):
        _check_gguf_arch(model_asset)

    fallback_tokenizer: Path | None = None
    for key in _TOKENIZER_PATH_KEYS:
        raw = payload.get(key)
        if isinstance(raw, str) and raw.strip():
            resolved = _resolve_ref(raw.strip(), base_tuple)
            if resolved is not None:
                fallback_tokenizer = resolved
                break

    cfg_ctx = payload.get("context")
    existing_ctx = cfg_ctx if type(cfg_ctx) is int and cfg_ctx > 0 else None
    resolved_context = context_size or existing_ctx or 8192

    derived_payload: dict | None = None
    config_path = config_file
    if context_size is not None and existing_ctx != context_size:
        derived_payload = dict(payload)
        derived_payload["context"] = context_size
        if asset_key is not None:
            derived_payload[asset_key] = str(model_asset)
        config_path = out_dir / "strata-candidate.json"
    return model_asset, config_path, derived_payload, resolved_context, fallback_tokenizer


def resolve_polystrata_model(
    installation: dict,
    model_input: Path,
    out_dir: Path,
    context_size: int | None,
) -> tuple[Path, Path, dict | None, int, Path | None]:
    """Resolve a GGUF, strata-*.json, or pack dir into (asset, config_path, derived, ctx, tok)."""
    if model_input.is_file():
        suffix = model_input.suffix.lower()
        if suffix == ".gguf":
            metadata = _check_gguf_arch(model_input)
            resolved_context = context_size or 8192
            display_name = metadata.get("general.name") or model_input.stem
            derived = {
                "engine": "strata-poly",
                "arch": metadata["general.architecture"],
                "model": display_name,
                "gguf": str(model_input),
                "context": resolved_context,
            }
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
            p for p in model_input.iterdir() if p.is_file() and p.name.startswith("strata") and p.suffix == ".json"
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
    declared = read_build_backend(Path(installation["working_directory"]))
    if declared in ("cuda", "hip", "cpu"):
        return declared
    from inferyard.platforms.device_gpu import gpu_snapshot

    gpu = gpu_snapshot()
    if gpu.get("status") == "observed":
        backends = {
            dev.get("backend")
            for dev in gpu.get("devices") or ()
            if isinstance(dev, dict) and dev.get("backend")
        }
        if "cuda" in backends:
            return "cuda"
        if "hip" in backends:
            return "hip"
    return "cpu"
