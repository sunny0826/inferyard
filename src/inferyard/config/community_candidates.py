"""Prepare unbound platform candidates using the frozen, existing platform recipes."""

from pathlib import Path

from inferyard.config.bundle import require_review
from inferyard.config.community_assets import libraries, model_template
from inferyard.config.loader import load_config
from inferyard.config.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    finish,
    new_path,
    read_json,
    require_platform,
    sha256,
)
from inferyard.config.preparation_io import asset_identity as hash_file
from inferyard.config.toml_writer import render
from inferyard.contracts.validation import validate_document
from inferyard.platforms.runtime_profiles import RELEASE, VERSION_PATTERN
from inferyard.platforms.runtime_verification import query_version, verify_receipt


def selected_preflight(path, system):
    report = read_json(path)
    if type(report) is not dict:
        raise PreparationError("invalid_input")
    if "details" in report:
        report = report["details"]
    if (
        type(report) is not dict
        or type(report.get("hardware")) is not dict
        or report["hardware"].get("platform") != system
    ):
        raise PreparationError("device_preflight_blocked")
    recommendation = report.get("recommendation")
    if type(recommendation) is not dict or recommendation.get("status") != "recommended":
        raise PreparationError("device_preflight_blocked")
    selected = recommendation.get("selected")
    if (
        type(selected) is not dict
        or type(selected.get("model_path")) is not str
        or not selected["model_path"]
    ):
        raise PreparationError("invalid_input")
    for key in ("threads", "context_size", "min_available_memory_bytes", "min_disk_bytes"):
        if type(selected.get(key)) is not int or selected[key] <= 0:
            raise PreparationError("invalid_input")
    allowed = ("cpu", "metal") if system == "Darwin" else ("cpu", "cuda")
    if selected.get("mode") not in allowed:
        raise PreparationError("device_preflight_blocked")
    if selected["mode"] == "cuda" and (
        type(selected.get("gpu_index")) is not int or selected["gpu_index"] < 0
    ):
        raise PreparationError("invalid_input")
    return selected


def fresh_macos(selected, results):
    from inferyard.platforms.device_preflight import (
        _disk_ancestor,
        discover_models,
        hardware_snapshot,
        recommend,
    )

    hardware = hardware_snapshot(_disk_ancestor(results))
    fresh = recommend(hardware, discover_models([], selected["model_path"]))
    if fresh["status"] != "recommended":
        raise PreparationError("device_preflight_blocked")
    current = fresh["selected"]
    if current["mode"] != selected["mode"]:
        raise PreparationError("device_selection_changed_repeat_device_check")
    if hardware.get("ac_online") is not True:
        raise PreparationError("ac_power_required")
    return current


def create(request):
    system, _ = require_platform(("Darwin", "arm64"), ("Windows", "x64"))
    if (
        system == "Darwin" and (request.engine_path is None or request.runtime_receipt is not None)
    ) or (
        system == "Windows" and (request.runtime_receipt is None or request.engine_path is not None)
    ):
        raise PreparationError("invalid_input")
    inputs = (
        request.preflight,
        request.bundle_path,
        request.engine_path,
        request.runtime_receipt,
        request.model_source_record,
    )
    out = new_path(request.out, inputs)
    results = request.output_root.resolve()
    if results.is_relative_to(out) or results.is_relative_to(Path(__file__).resolve().parents[1]):
        raise PreparationError("invalid_input")
    if results.exists() and not results.is_dir():
        raise PreparationError("invalid_input")
    selected = selected_preflight(request.preflight, system)
    bundle = read_json(request.bundle_path)
    require_review(bundle)
    if system == "Darwin":
        selected = fresh_macos(selected, results)
        engine_path = request.engine_path.resolve(strict=True)
        library_map = libraries(engine_path, ("*.dylib",))
        if len(library_map) == 1 or (
            selected["mode"] == "metal" and "libggml-metal.dylib" not in library_map
        ):
            raise PreparationError("invalid_library_manifest")
        manifest_path = out / "engine-sha256.json"
    else:
        profile, engine_path, manifest_path = verify_receipt(
            request.runtime_receipt, mode=selected["mode"]
        )
        library_map = profile["libraries"]
    query_version(engine_path, VERSION_PATTERN)
    if system == "Windows":
        verify_receipt(request.runtime_receipt, mode=selected["mode"])
    elif library_map != libraries(engine_path, ("*.dylib",)):
        raise PreparationError("runtime_asset_mismatch")
    engine = hash_file(engine_path)
    model = hash_file(Path(selected["model_path"]))
    new_path(request.out, (*inputs, model.path))
    metadata, template_raw = model_template(model.path)
    template_path = out / "chat-template.jinja"
    from inferyard.platforms.model_source import source_declaration

    repo, revision = source_declaration(
        request.model_source_record, model, repo=request.model_repo, revision=request.model_revision
    )
    shared = dict(repo=repo, revision=revision, port=request.port)
    if system == "Darwin":
        from inferyard.config.community_candidates_macos import build_config
        from inferyard.platforms.identity import environment_snapshot

        config = build_config(
            selected,
            model,
            metadata,
            template_raw.decode(),
            template_path,
            manifest_path,
            engine,
            RELEASE,
            request.bundle_path.resolve(),
            bundle["version"],
            results,
            environment_snapshot(),
            **shared,
        )
    else:
        from inferyard.config.community_candidates_windows import build_config

        receipt = {"release": RELEASE, "runtime_library_manifest": str(manifest_path)}
        config = build_config(
            selected,
            model,
            metadata,
            template_raw.decode(),
            template_path,
            engine,
            receipt,
            request.bundle_path.resolve(),
            bundle["version"],
            results,
            **shared,
        )
    validate_document("config", config)
    try:
        content = render(config)
    except ValueError:
        raise PreparationError("serialization_mismatch", 4) from None
    output = NewDirectory(request.out, (*inputs, model.path))
    output.write("chat-template.jinja", template_raw)
    if hash_file(template_path).sha256 != sha256(template_raw):
        raise PreparationError("serialization_mismatch", 4)
    if system == "Darwin":
        output.json("engine-sha256.json", library_map)
    candidate = output.write("candidate.toml", content.encode())
    if load_config(candidate).config.to_dict() != config:
        raise PreparationError("serialization_mismatch", 4)
    details = {
        **base_details(output.path),
        "candidate": str(candidate),
        "template": str(template_path),
        "runtime_library_manifest": str(manifest_path),
        "device_preflight": str(request.preflight.resolve()),
        "bundle": str(request.bundle_path.resolve()),
        "results": str(results),
        "model": model.path,
        "model_sha256": model.sha256,
        "engine_sha256": engine.sha256,
        "mode": selected["mode"],
        "preparation": str(output.path / "preparation.json"),
    }
    return finish(output, "preparation.json", "config_candidate.v1", details)
