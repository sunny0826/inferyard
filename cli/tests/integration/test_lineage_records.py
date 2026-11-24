"""Portable raw receipt helpers shared with unit lineage tests."""

import hashlib
import json
from copy import deepcopy


def encoded(record):
    text = json.dumps(record, indent=2) + "\n"
    return {"text": text, "sha256": hashlib.sha256(text.encode()).hexdigest()}


def records(declaration):
    conversion = {
        k: deepcopy(declaration[k])
        for k in ("base_repo", "base_revision", "base_artifacts", "tokenizer_sha256")
    }
    conversion.update(
        definition="conversion_receipt.v1",
        tool_sha256=declaration["conversion_tool_sha256"],
        recipe_sha256=declaration["conversion_recipe_sha256"],
        output_sha256="9" * 64,
        exit_code=0,
    )
    quantization = dict(
        definition="quantization_receipt.v1",
        input_sha256=conversion["output_sha256"],
        tool_sha256=declaration["quantization_tool_sha256"],
        recipe_sha256=declaration["quantization_recipe_sha256"],
        output_sha256=declaration["output_sha256"],
        packing=declaration["packing"],
        exit_code=0,
    )
    return {"conversion": encoded(conversion), "quantization": encoded(quantization)}
