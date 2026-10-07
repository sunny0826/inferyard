"""The existing macOS preparation recipe, independent of repository paths."""

import hashlib
from copy import deepcopy
from pathlib import Path

from inferyard.contracts.schemas import CONFIG_DEFAULTS


def build_config(
    current,
    model,
    metadata,
    template,
    template_path,
    manifest_path,
    engine,
    release,
    bundle_path,
    bundle_version,
    results,
    environment,
    *,
    repo="local",
    revision=None,
    port=48857,
):
    config = deepcopy(CONFIG_DEFAULTS)
    config.update(
        schema_version=3,
        bundle={
            "path": str(bundle_path),
            "version": bundle_version,
        },
        device={"id": "LAB-MACOS-001", "client_position": "same_host_native"},
        endpoint={"url": f"http://127.0.0.1:{port}", "server_pid": 1, "process_start_ticks": 1},
        engine={
            "adapter": "prism_llama_server_v1",
            "release": release,
            "backend": current["mode"],
            "binary_path": engine.path,
            "binary_sha256": engine.sha256,
            "runtime_library_manifest": str(manifest_path),
        },
        model={
            "display_name": metadata.get("general.name") or Path(model.path).stem,
            "repo": repo,
            "revision": revision or model.sha256,
            "local_path": model.path,
            "sha256": model.sha256,
            "packing": next(
                (name for name in ("PTQ1_0", "PQ2_0") if name in Path(model.path).name), "GGUF"
            ),
            "template_path": str(template_path),
            "template_sha256": hashlib.sha256(template.encode()).hexdigest(),
        },
        generation={
            "max_tokens": 512,
            "seed": 42,
            "seed_support": "supported",
            "reasoning_mode": "off",
            "temperature": 0.7,
            "top_p": 0.8,
            "top_k": 20,
            "min_p": 0.0,
            "presence_penalty": 1.5,
            "repeat_penalty": 1.0,
            "stop": [],
        },
        conditions={
            "context_size": current["context_size"],
            "threads": current["threads"],
            "threads_batch": current["threads"],
            "slots": 1,
            "cache_policy": "disabled",
            "ac_online": True,
            "profile": "unknown",
            "governor": "unknown",
            "epp": "unknown",
            "allow_unknown_environment": True,
            "model_loaded": True,
            "background_load": "macOS desktop; recheck actual load before freezing a trial",
        },
    )
    config["telemetry"]["service_rss_required"] = True
    config["conditions"]["profile"] = environment["profile"]
    if environment.get("macos_power_policy") is not None:
        config["conditions"]["macos_power_policy"] = environment["macos_power_policy"]
    config["engine"]["startup_args"] = [
        "-m",
        model.path,
        "-ngl",
        "99" if current["mode"] == "metal" else "0",
        "-t",
        str(current["threads"]),
        "-tb",
        str(current["threads"]),
        "-c",
        str(current["context_size"]),
        "-np",
        "1",
        "--host",
        "127.0.0.1",
        "--port",
        str(port),
        "--no-webui",
        "--reasoning",
        "off",
        "--jinja",
        "--chat-template-kwargs",
        '{"enable_thinking":false}',
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
        "--slots",
    ]
    config["output"].update(
        root=str(results),
        min_available_memory_bytes=current["min_available_memory_bytes"],
        min_disk_bytes=current["min_disk_bytes"],
    )
    return config
