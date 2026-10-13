"""Inspect a trusted local rebuild using the pinned converter's own GGUF reader.

Run with the isolated conversion Python. This records structural evidence; it
does not promote output or authenticate conversion lineage.
"""

import argparse
import hashlib
import json
import sys
from collections import Counter
from pathlib import Path

from qwen3_tensor_inventory import check_storage_types

SOURCE = Path(__file__).resolve().parents[1] / (
    "artifacts/phase2-quantization/source/"
    "llama.cpp-adfffbe41b2cabcd51fff326ab045662265062bb/gguf-py"
)


def inspect(path, expected_type):
    sys.path.insert(0, str(SOURCE))
    import gguf

    if not Path(gguf.__file__).resolve().is_relative_to(SOURCE.resolve()):
        raise ValueError("unexpected_gguf_reader")
    before = path.stat()
    reader = gguf.GGUFReader(path, "r")
    metadata = {}
    for key in (
        "general.architecture",
        "general.file_type",
        "qwen3.block_count",
        "qwen3.embedding_length",
        "qwen3.context_length",
    ):
        field = reader.get_field(key)
        if field is None:
            raise ValueError("missing_gguf_metadata:" + key)
        metadata[key] = field.contents()
    if metadata["general.architecture"] != "qwen3":
        raise ValueError("unexpected_architecture")
    if metadata["general.file_type"] != {"BF16": 32, "Q4_K_M": 15, "Q8_0": 7}[expected_type]:
        raise ValueError("unexpected_file_type")
    if not reader.tensors:
        raise ValueError("empty_tensor_inventory")
    rows = []
    end = reader.data_offset
    for tensor in sorted(reader.tensors, key=lambda item: item.data_offset):
        offset, size = int(tensor.data_offset), int(tensor.n_bytes)
        if (
            offset < end
            or offset % reader.alignment
            or size <= 0
            or offset + size > before.st_size
            or any(int(v) <= 0 for v in tensor.shape)
        ):
            raise ValueError("invalid_tensor_geometry:" + tensor.name)
        rows.append(
            {
                "name": tensor.name,
                "type": tensor.tensor_type.name,
                "shape": tensor.shape.tolist(),
                "bytes": size,
                "offset": offset,
                "elements": int(tensor.n_elements),
            }
        )
        end = offset + size
    check_storage_types(rows, expected_type)
    field_hashes = {}
    for name, field in reader.fields.items():
        if name.startswith("GGUF."):
            continue
        canonical = json.dumps(
            {"types": [int(value) for value in field.types], "value": field.contents()},
            sort_keys=True,
            ensure_ascii=False,
            separators=(",", ":"),
            allow_nan=False,
        ).encode("utf-8")
        field_hashes[name] = hashlib.sha256(canonical).hexdigest()
    tokenizer_hashes = {
        name: digest for name, digest in field_hashes.items() if name.startswith("tokenizer.")
    }
    sha = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(4 * 1024**2), b""):
            sha.update(chunk)
    after = path.stat()
    if any(
        getattr(before, key) != getattr(after, key)
        for key in ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns")
    ):
        raise ValueError("gguf_changed_during_inspection")
    return {
        "metadata": metadata,
        "tensors": rows,
        "sha256": sha.hexdigest(),
        "bytes": before.st_size,
        "tensor_types": dict(Counter(r["type"] for r in rows)),
        "metadata_field_sha256": field_hashes,
        "tokenizer_field_sha256": tokenizer_hashes,
        "metadata_hash_encoding": "utf8_json_sorted_keys_compact_unescaped_unicode_type_and_value",
        "reader_module": str(Path(gguf.__file__).resolve()),
        "output_accepted": False,
        "limitations": [
            "same_reader_family_as_converter_not_independent_validation",
            "base_config_and_expected_tensor_inventory_matching_still_required",
            "no_inference_or_numeric_equivalence_check",
        ],
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--type", choices=["BF16", "Q4_K_M", "Q8_0"], required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--base-config", type=Path)
    args = parser.parse_args()
    result = inspect(args.input, args.type)
    if args.base_config:
        from qwen3_tensor_inventory import check_inventory

        result["base_inventory_check"] = check_inventory(
            result, json.loads(args.base_config.read_text())
        )
        result["limitations"].remove(
            "base_config_and_expected_tensor_inventory_matching_still_required"
        )
    with args.out.open("x") as stream:
        json.dump(result, stream, indent=2)
        stream.write("\n")
