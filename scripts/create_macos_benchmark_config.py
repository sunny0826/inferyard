"""Create a macOS candidate from local assets; never download or start a service."""

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

from inferyard.config.loader import load_config
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import read_json
from inferyard.platforms.device_preflight import (
    _disk_ancestor,
    discover_models,
    hardware_snapshot,
    recommend,
)
from inferyard.platforms.gguf_metadata import read_metadata
from inferyard.platforms.identity import environment_snapshot, hash_file

ROOT = Path(__file__).resolve().parents[1]


def verify_engine(path):
    result = subprocess.run(
        [str(path), "--version"], capture_output=True, text=True, timeout=10, check=True
    )
    if not re.search(r"\b10743\b.*\badfffbe(?:41)?\b", result.stdout + result.stderr):
        raise ValueError("unsupported_engine_build_requires_adapter_validation")
    return "prism-b10743-adfffbe"


def render_toml(config):
    lines = [
        "# macOS candidate; bind an externally started service before measurement.",
        "# Local assets and resource estimates do not establish model acceptance.",
        "schema_version = 3",
    ]
    for section, values in config.items():
        if isinstance(values, dict):
            lines.extend(["", f"[{section}]"])
            lines.extend(
                f"{key} = {json.dumps(value, ensure_ascii=False)}"
                for key, value in values.items()
                if not isinstance(value, dict)
            )
            for key, value in values.items():
                if isinstance(value, dict):
                    lines.extend(["", f"[{section}.{key}]"])
                    lines.extend(
                        f"{name} = {json.dumps(item, ensure_ascii=False)}"
                        for name, item in value.items()
                    )
    return "\n".join(lines) + "\n"


def create(preflight, engine_path, out, *, repo="local", revision=None, port=48857):
    if sys.platform != "darwin":
        raise ValueError("native_macos_required")
    if type(port) is not int or not 1 <= port <= 65535:
        raise ValueError("invalid_port")
    out, engine_path = out.resolve(), engine_path.resolve(strict=True)
    template_path = out.with_suffix(".chat-template.jinja")
    manifest_path = out.with_suffix(".libraries.json")
    preparation_path = out.with_suffix(".preparation.json")
    paths = (out, template_path, manifest_path, preparation_path)
    if any(path.exists() for path in paths):
        raise FileExistsError("candidate_artifact_already_exists")
    report = read_json(preflight)
    report = report.get("details", report)
    if report.get("hardware", {}).get("platform") != "Darwin":
        raise ValueError("macos_device_preflight_required")
    selected = report["recommendation"]["selected"]
    if report["recommendation"]["status"] != "recommended" or not selected:
        raise ValueError("device_preflight_blocked")
    if selected["mode"] not in ("cpu", "metal"):
        raise ValueError("unsupported_macos_backend")
    # The saved estimate is not permission to use stale free-memory observations.
    hardware = hardware_snapshot(_disk_ancestor(out.parent))
    models = discover_models([], selected["model_path"])
    fresh = recommend(hardware, models)
    if fresh["status"] != "recommended":
        raise ValueError("current_device_preflight_blocked:" + str(fresh["reason"]))
    current = fresh["selected"]
    if current["mode"] != selected["mode"]:
        raise ValueError("device_selection_changed_repeat_device_check")
    if hardware.get("ac_online") is not True:
        raise ValueError("ac_power_required")
    release = verify_engine(engine_path)
    engine = hash_file(engine_path)
    model = hash_file(Path(current["model_path"]))
    metadata = read_metadata(model.path)
    template = metadata.get("tokenizer.chat_template") or metadata.get(
        "tokenizer.chat_template.default"
    )
    if not isinstance(template, str) or not template.strip():
        raise ValueError("model_chat_template_required")
    libraries = {path.name: hash_file(path).sha256 for path in engine_path.parent.glob("*.dylib")}
    if not libraries or (current["mode"] == "metal" and "libggml-metal.dylib" not in libraries):
        raise ValueError("dynamic_runtime_libraries_required")
    # Include the executable so a CPU-only dynamic build still has a nonempty manifest.
    libraries[engine_path.name] = engine.sha256
    from inferyard.config.community_candidates_macos import build_config

    config = build_config(
        current,
        model,
        metadata,
        template,
        template_path,
        manifest_path,
        engine,
        release,
        ROOT / "bundles/zh-smoke.json",
        read_json(ROOT / "bundles/zh-smoke.json")["version"],
        ROOT / "results",
        environment_snapshot(),
        repo=repo,
        revision=revision,
        port=port,
    )
    validate_document("config", config)
    preparation = {
        "kind": "macos_candidate_preparation.v1",
        "device_preflight": str(preflight.resolve()),
        "hardware": hardware,
        "recommendation": fresh,
        "model_requests_sent": 0,
        "ready_to_run": False,
        "limitations": ["candidate_requires_live_binding_and_probe", "performance_not_qualified"],
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    for path, content in (
        (template_path, template),
        (manifest_path, json.dumps(libraries, indent=2) + "\n"),
        (preparation_path, json.dumps(preparation, ensure_ascii=False, indent=2) + "\n"),
        (out, render_toml(config)),
    ):
        with path.open("x", encoding="utf-8", newline="\n") as stream:
            stream.write(content)
    load_config(out)
    return {"candidate": str(out), "model": model.path, "mode": current["mode"], **preparation}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preflight", type=Path, required=True)
    parser.add_argument("--engine", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--model-repo", default="local")
    parser.add_argument("--model-revision")
    parser.add_argument("--port", type=int, default=48857)
    args = parser.parse_args()
    result = create(
        args.preflight,
        args.engine,
        args.out,
        repo=args.model_repo,
        revision=args.model_revision,
        port=args.port,
    )
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    main()
