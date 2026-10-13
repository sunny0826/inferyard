"""The existing Windows recommendation recipe, with explicit caller-owned paths."""

import hashlib
from copy import deepcopy
from pathlib import Path

from inferyard.contracts.schemas import CONFIG_DEFAULTS


def build_config(
    selected,
    model,
    metadata,
    template,
    template_path,
    engine,
    receipt,
    bundle_path,
    bundle_version,
    results,
    *,
    repo="local",
    revision=None,
    port=48857,
):
    mode = selected["mode"]
    config = deepcopy(CONFIG_DEFAULTS)
    config.update(
        schema_version=3,
        bundle={
            "path": str(bundle_path),
            "version": bundle_version,
        },
        device={"id": "LAB-WINDOWS-001", "client_position": "same_host_native"},
        endpoint={"url": f"http://127.0.0.1:{port}", "server_pid": 1, "process_start_ticks": 1},
        engine={"adapter": "prism_llama_server_v1"},
        model={},
        generation={},
        conditions={
            "ac_online": True,
            "allow_unknown_environment": True,
            "model_loaded": True,
            "cache_policy": "disabled",
            "slots": 1,
            "epp": "unknown",
            "governor": "unknown",
            "profile": "unknown",
        },
        evidence={"parameter_evidence": [], "service_evidence": []},
    )
    config["execution"].update(
        probe_prompt="请只用中文回答：中国的首都是哪里？只回答城市名。",
        warmup_prompt="请只回答：准备就绪",
    )
    config["generation"].update(
        max_tokens=512,
        seed=42,
        seed_support="supported",
        reasoning_mode="off",
        repeat_penalty=1.0,
        stop=[],
    )
    config["engine"].update(
        binary_path=engine.path,
        binary_sha256=engine.sha256,
        backend=mode,
        release=receipt.get("release", receipt.get("engine_release")),
        runtime_library_manifest=receipt["runtime_library_manifest"],
    )
    packing = {15: "Q4_K_M", 7: "Q8_0", 0: "F32", 1: "F16"}.get(
        metadata.get("general.file_type"), "GGUF"
    )
    config["model"].update(
        display_name=(metadata.get("general.name") or Path(model.path).stem) + " " + packing,
        repo=repo,
        revision=revision or model.sha256,
        local_path=model.path,
        sha256=model.sha256,
        packing=packing,
        template_path=str(template_path),
        template_sha256=hashlib.sha256(template.encode()).hexdigest(),
    )
    config["device"]["id"] = "LAB-WINDOWS-GPU-001" if mode == "cuda" else "LAB-WINDOWS-001"
    threads, context = selected["threads"], selected["context_size"]
    config["conditions"].update(
        threads=threads,
        threads_batch=threads,
        context_size=context,
        background_load=(
            "Windows desktop, remote display and benchmark operator; "
            "existing OS background activity remains; GPU sampling every second"
        ),
    )
    config["generation"].update(
        temperature=0.0, top_p=1.0, top_k=0, min_p=0.0, presence_penalty=0.0
    )
    config["engine"]["startup_args"] = [
        "-m",
        model.path,
        "-ngl",
        "999" if mode == "cuda" else "0",
        "-t",
        str(threads),
        "-tb",
        str(threads),
        "-c",
        str(context),
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
        "--log-verbosity",
        "4",
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
        "--slots",
        *(["--device", "CUDA" + str(selected["gpu_index"])] if mode == "cuda" else []),
    ]
    config["output"].update(
        min_available_memory_bytes=selected["min_available_memory_bytes"],
        min_disk_bytes=selected["min_disk_bytes"],
        root=str(results),
    )
    return config
