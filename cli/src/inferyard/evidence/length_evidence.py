"""Offline integrity and semantic checks before reusing a prepared prompt."""

import hashlib
import re
from pathlib import Path

from inferyard.config.loader import validate_runtime_config
from inferyard.evidence.storage import EvidenceError, read_json, sha256_file
from inferyard.runtime.length_builder import validate_spec

REQUIRED = {
    "spec.json",
    "config.frozen.json",
    "service-binding.json",
    "service.props.json",
    "identity.json",
    "probes.json",
    "result.json",
    "prompt.txt",
}


def read_preparation(directory):
    """Verify a matched preparation; hashes prove consistency, not authenticity."""
    root = Path(directory).resolve()
    manifest = read_json(root / "preparation-manifest.json")
    files = manifest.get("files")
    if not isinstance(files, dict) or set(files) != REQUIRED:
        raise EvidenceError("length_preparation_inventory_invalid")
    for name, digest in files.items():
        path = root / name
        if path.is_symlink() or not path.is_file() or sha256_file(path) != digest:
            raise EvidenceError("length_preparation_hash_mismatch")
    spec = read_json(root / "spec.json")
    validate_spec(spec)
    config = read_json(root / "config.frozen.json")
    validate_runtime_config(config)
    identity = read_json(root / "identity.json")
    props = read_json(root / "service.props.json")
    recorded = {item["path"]: item["sha256"] for item in identity.get("files", [])}
    template = props.get("chat_template")
    if (
        identity.get("verification") != "verified"
        or not isinstance(template, str)
        or hashlib.sha256(template.encode()).hexdigest() != config["model"]["template_sha256"]
        or props.get("model_path") != config["model"]["local_path"]
        or any(
            recorded.get(config[section][path_key]) != config[section][hash_key]
            for section, path_key, hash_key in (
                ("model", "local_path", "sha256"),
                ("model", "template_path", "template_sha256"),
                ("engine", "binary_path", "binary_sha256"),
            )
        )
    ):
        raise EvidenceError("length_preparation_identity_mismatch")
    result = read_json(root / "result.json")
    probes = read_json(root / "probes.json")
    if (
        result.get("status") != "matched"
        or result.get("scope") != "model_specific_performance_text_not_shared_quality_corpus"
        or not isinstance(probes, list)
        or not 1 <= len(probes) <= spec["max_probes"]
        or result.get("probes") != probes
        or result.get("selected") != probes[-1]
    ):
        raise EvidenceError("length_preparation_result_invalid")
    seen = set()
    for record in probes:
        repetitions = record.get("repetitions")
        if (
            type(repetitions) is not int
            or not 0 <= repetitions <= spec["max_repetitions"]
            or repetitions in seen
        ):
            raise EvidenceError("length_preparation_repetitions_invalid")
        seen.add(repetitions)
        prompt = spec["prefix"] + spec["padding_unit"] * repetitions + spec["suffix"]
        if len(prompt) > spec["max_characters"] or hashlib.sha256(
            prompt.encode()
        ).hexdigest() != record.get("prompt_sha256"):
            raise EvidenceError("length_preparation_prompt_invalid")
        measurement = record.get("measurement", {})
        count = measurement.get("input_tokens")
        if (
            type(count) is not int
            or count < 0
            or measurement.get("verification") != "verified"
            or measurement.get("source") != "apply-template+tokenize:add_special,parse_special"
            or measurement.get("output_budget") != config["generation"]["max_tokens"]
            or not re.fullmatch(r"[0-9a-f]{64}", str(measurement.get("template_prompt_sha256")))
        ):
            raise EvidenceError("length_preparation_measurement_invalid")
    if (root / "prompt.txt").read_bytes() != prompt.encode() or abs(
        count - spec["target_tokens"]
    ) > spec["tolerance_tokens"]:
        raise EvidenceError("length_preparation_target_mismatch")
    return {
        "config": config,
        "spec": spec,
        "prompt": prompt,
        "result": result,
        "source": {
            "path": str(root / "preparation-manifest.json"),
            "sha256": sha256_file(root / "preparation-manifest.json"),
        },
    }
