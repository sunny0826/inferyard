"""Bounded single-file GGUF/native-v3 inspection after Windows byte identity binding.

Native framing source: JGamboa/ninfer-4090-windows c6adc56, artifact-container.md §3–4.
Architecture-specific tensor interpretation remains the engine loader's responsibility.
"""

import hashlib
import os
import struct
from pathlib import Path

from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError

MAX_DIRECTORY_BYTES = 16 * 1024**2


def inspect_asset(config):
    model = config["model"]
    try:
        if model["kind"] == "gguf":
            from inferyard.platforms.gguf_metadata import read_metadata

            metadata = read_metadata(model["local_path"], extra_keys=("split.count",))
            if metadata.get("split.count", 1) != 1:
                raise PreflightError("lab_split_model_unsupported")
            return {"kind": "gguf", "source": "GGUF:metadata", "metadata": metadata}
        with Path(model["local_path"]).open("rb") as stream:
            header = stream.read(32)
            if len(header) != 32 or header[:8] != b"NINFER\0\x03":
                raise PreflightError("lab_native_container_header_invalid")
            length = struct.unpack("<Q", header[8:16])[0]
            if not 0 < length <= MAX_DIRECTORY_BYTES:
                raise PreflightError("lab_native_directory_bounds")
            directory = strict_json_loads(stream.read(length).decode("utf-8"))
            size = os.fstat(stream.fileno()).st_size
        return inspect_native(directory, header[16:].hex(), length, size, model)
    except (OSError, ValueError, UnicodeError, ContractError, KeyError, TypeError) as exc:
        raise PreflightError("lab_asset_invalid") from exc


def inspect_native(directory, artifact_id, length, size, model):
    required = {"components", "objects", "bindings", "uses", "files"}
    if (
        type(directory) is not dict
        or not required <= directory.keys()
        or not directory.keys() <= required | {"metadata", "provenance"}
    ):
        raise PreflightError("lab_native_directory_invalid")
    components, files = directory["components"], directory["files"]
    if (
        type(components) is not dict
        or "text" not in components
        or any(
            type(v) is not dict or type(v.get("config")) is not dict for v in components.values()
        )
    ):
        raise PreflightError("lab_native_components_invalid")
    if type(files) is not list or len(files) != 1:
        raise PreflightError("lab_split_model_unsupported")
    entry = files[0]
    if (
        type(entry) is not dict
        or set(entry) != {"path", "payload_bytes"}
        or entry["path"] is not None
        or type(entry["payload_bytes"]) is not int
        or not 0 < entry["payload_bytes"] < 2**64
    ):
        raise PreflightError("lab_native_directory_invalid")
    payload_start = (32 + length + 4095) // 4096 * 4096
    if size != payload_start + entry["payload_bytes"] or size != model["bytes"]:
        raise PreflightError("lab_native_directory_bounds")
    objects = directory["objects"]
    if type(objects) is not list or not objects or len(objects) > 100_000:
        raise PreflightError("lab_native_objects_invalid")
    previous_end = 0
    ids = set()
    for obj in objects:
        if type(obj) is not dict:
            raise PreflightError("lab_native_objects_invalid")
        offset, count, identifier = obj.get("offset"), obj.get("bytes"), obj.get("id")
        if (
            type(offset) is not int
            or type(count) is not int
            or count <= 0
            or offset < previous_end
            or offset + count > entry["payload_bytes"]
            or type(identifier) is not str
            or not identifier
            or identifier in ids
        ):
            raise PreflightError("lab_native_objects_invalid")
        previous_end = offset + count
        ids.add(identifier)
    if type(directory["bindings"]) is not dict or type(directory["uses"]) is not list:
        raise PreflightError("lab_native_directory_invalid")
    with Path(model["component_ledger_path"]).open("rb") as stream:
        raw = stream.read(MAX_DIRECTORY_BYTES + 1)
    if len(raw) > MAX_DIRECTORY_BYTES:
        raise PreflightError("lab_component_ledger_invalid")
    ledger = strict_json_loads(raw.decode("utf-8"))
    expected = {
        "artifact_id": artifact_id,
        "model_sha256": model["sha256"],
        "components_sha256": hashlib.sha256(json_bytes(components)).hexdigest(),
    }
    if ledger != expected:
        raise PreflightError("lab_component_ledger_mismatch")
    return {
        "kind": "ninfer",
        "version": 3,
        "artifact_id": artifact_id,
        "components": sorted(components),
        "component_ledger": ledger,
        "source": "NINFER:v3:single_file",
    }
