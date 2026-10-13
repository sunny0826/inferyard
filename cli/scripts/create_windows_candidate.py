"""Create a new native Windows candidate from verified D: runtime assets."""

import argparse
import hashlib
import json
import os
from datetime import UTC, datetime
from pathlib import Path

from collect_windows_machine import collect_machine

from inferyard.config.loader import load_config
from inferyard.config.toml_writer import render
from inferyard.evidence.storage import read_json
from inferyard.platforms.gguf_metadata import read_metadata
from inferyard.platforms.identity import hash_file

ROOT = Path(__file__).resolve().parents[1]


def render_toml(config):
    return render(
        config,
        comment=(
            "Native Windows candidate; bind to an externally started service before measurement."
        ),
    )


def model_declaration(model, receipt, template_path):
    """Bind the verified model and its own embedded template, without old evidence."""
    metadata = read_metadata(model.path)
    template = metadata.get("tokenizer.chat_template") or metadata.get(
        "tokenizer.chat_template.default"
    )
    if not isinstance(template, str) or not template.strip():
        raise ValueError("model_chat_template_required")
    return {
        "local_path": model.path,
        "sha256": model.sha256,
        "revision": receipt["model_revision"],
        "template_path": str(template_path),
        "template_sha256": hashlib.sha256(template.encode("utf-8")).hexdigest(),
    }, template


def create_candidate(out, *, minimum_memory_bytes=8 * 1024**3):
    if os.name != "nt":
        raise ValueError("native_windows_required")
    if type(minimum_memory_bytes) is not int or minimum_memory_bytes <= 0:
        raise ValueError("positive_memory_budget_required")
    out = out.resolve()
    if out.drive.upper() != "D:" or not out.is_relative_to(ROOT):
        raise ValueError("candidate_must_stay_in_D_workspace")
    if out.exists():
        raise FileExistsError("candidate_already_exists")
    template_path = out.with_suffix(".chat-template.jinja")
    if template_path.exists():
        raise FileExistsError("chat_template_already_exists")
    receipt = read_json(ROOT / ".tools/downloads/windows-runtime.json")
    if receipt.get("model_verified") is not True:
        raise ValueError("verified_runtime_assets_required")
    model = hash_file(Path(receipt["model_path"]), receipt["model_sha256"])
    engine = hash_file(Path(receipt["engine_binary"]), receipt["engine_sha256"])
    for name, expected in receipt["libraries"].items():
        if Path(name).name != name:
            raise ValueError("invalid_runtime_library_name")
        hash_file(Path(engine.path).parent / name, expected)
    config = load_config(ROOT / "configs/qwen3-4b.windows.example.toml").config.to_dict()
    config["engine"].update(
        binary_path=engine.path,
        binary_sha256=engine.sha256,
        release=receipt["engine_release"],
        runtime_library_manifest=receipt["runtime_library_manifest"],
    )
    declaration, template = model_declaration(model, receipt, template_path)
    config["model"].update(declaration)
    arguments = config["engine"]["startup_args"]
    for index, argument in enumerate(arguments[:-1]):
        if argument in ("-m", "--model"):
            arguments[index + 1] = model.path
    config["conditions"]["background_load"] = (
        "Windows desktop and Lody agent; no concurrent project builds, tests or model downloads "
        "during measurement; ordinary OS background services remain"
    )
    config["output"]["min_available_memory_bytes"] = minimum_memory_bytes
    with template_path.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(template)
    with out.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(render_toml(config))
    load_config(out)
    machine = collect_machine(
        ROOT
        / "validation/windows"
        / ("machine-" + out.stem + "-" + datetime.now(UTC).strftime("%Y%m%dT%H%M%S%fZ")),
        config_path=out,
    )
    return {
        "candidate": str(out),
        "model_sha256": model.sha256,
        "engine_sha256": engine.sha256,
        "minimum_available_memory_bytes": minimum_memory_bytes,
        "model_requests_sent": 0,
        "machine_preparation": machine,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument(
        "--min-available-memory-gib",
        type=int,
        default=8,
        help="explicit new-candidate budget; never changes the historical recipe",
    )
    args = parser.parse_args()
    result = create_candidate(
        out=args.out, minimum_memory_bytes=args.min_available_memory_gib * 1024**3
    )
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    main()
