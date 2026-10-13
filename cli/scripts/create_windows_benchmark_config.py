"""Create a model-independent Windows configuration from device-check recommendations."""

import argparse
import json
from pathlib import Path

from create_windows_candidate import render_toml
from prepare_windows_runtime import ROOT, require_destination

from inferyard.config.loader import load_config
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import read_json
from inferyard.platforms.gguf_metadata import read_metadata
from inferyard.platforms.identity import hash_file


def create(preflight, out):
    out = require_destination(out)
    if out.exists():
        raise FileExistsError("candidate_already_exists")
    report = read_json(preflight)
    selected = report["recommendation"]["selected"]
    if report["recommendation"]["status"] != "recommended" or not selected:
        raise ValueError("device_preflight_blocked")
    mode = selected["mode"]
    receipt = read_json(
        ROOT
        / ".tools/downloads"
        / ("windows-cuda-runtime.json" if mode == "cuda" else "windows-runtime.json")
    )
    engine_path = receipt.get("binary_path", receipt.get("engine_binary"))
    engine_sha = receipt.get("binary_sha256", receipt.get("engine_sha256"))
    engine = hash_file(Path(engine_path), engine_sha)
    model = hash_file(Path(selected["model_path"]))
    metadata = read_metadata(model.path)
    template = metadata.get("tokenizer.chat_template") or metadata.get(
        "tokenizer.chat_template.default"
    )
    if not isinstance(template, str) or not template.strip():
        raise ValueError("model_chat_template_required")
    template_path = out.with_suffix(".chat-template.jinja")
    if template_path.exists():
        raise FileExistsError("chat_template_already_exists")
    from inferyard.config.community_candidates_windows import build_config

    config = build_config(
        selected,
        model,
        metadata,
        template,
        template_path,
        engine,
        receipt,
        ROOT / "bundles/zh-smoke.json",
        read_json(ROOT / "bundles/zh-smoke.json")["version"],
        ROOT / "results",
    )
    validate_document("config", config)
    with template_path.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(template)
    with out.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(render_toml(config))
    load_config(out)
    return {
        "candidate": str(out),
        "mode": mode,
        "model": model.path,
        "device_preflight": str(preflight),
        "model_sha256": model.sha256,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preflight", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(create(args.preflight, args.out)))


if __name__ == "__main__":
    main()
