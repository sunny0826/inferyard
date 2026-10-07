"""Declared model lineage and receipt shapes, independent of configuration behavior."""

from inferyard.contracts.schemas_common import HASH, LABEL, array, enum, obj

REVISION = {"type": "string", "pattern": "^(?:[0-9a-f]{40}|[0-9a-f]{64})$"}
LINEAGE = obj(
    {
        "definition": enum("declared_quantization_lineage.v1"),
        "base_repo": LABEL,
        "base_revision": REVISION,
        "base_artifacts": array(obj({"name": LABEL, "sha256": HASH}), 1),
        "tokenizer_sha256": HASH,
        "conversion_tool_sha256": HASH,
        "conversion_recipe_sha256": HASH,
        "quantization_tool_sha256": HASH,
        "quantization_recipe_sha256": HASH,
        "output_sha256": HASH,
        "packing": LABEL,
    }
)
RAW_RECORD = obj(
    {
        "sha256": HASH,
        "text": {"type": "string", "minLength": 1, "maxLength": 1_000_000},
    }
)
RECORDS = obj({"conversion": RAW_RECORD, "quantization": RAW_RECORD})
CONVERSION = obj(
    {
        "definition": enum("conversion_receipt.v1"),
        "base_repo": LABEL,
        "base_revision": REVISION,
        "base_artifacts": array(obj({"name": LABEL, "sha256": HASH}), 1),
        "tokenizer_sha256": HASH,
        "tool_sha256": HASH,
        "recipe_sha256": HASH,
        "output_sha256": HASH,
        "exit_code": {"type": "integer", "const": 0},
    }
)
QUANTIZATION = obj(
    {
        "definition": enum("quantization_receipt.v1"),
        "input_sha256": HASH,
        "tool_sha256": HASH,
        "recipe_sha256": HASH,
        "output_sha256": HASH,
        "packing": LABEL,
        "exit_code": {"type": "integer", "const": 0},
    }
)
