"""Expected dense Qwen3 tensor shapes in GGUF dimension order, from base config."""

import re


def check_preserved_metadata(base, quantized):
    """Compare freshly inspected files; this alone is not lineage authentication."""
    maps = []
    for inspection in (base, quantized):
        fields = inspection["metadata_field_sha256"]
        required = {
            "general.architecture",
            "general.file_type",
            "tokenizer.ggml.tokens",
            "tokenizer.chat_template",
        }
        if not required <= fields.keys() or any(
            not isinstance(value, str) or re.fullmatch(r"[0-9a-f]{64}", value) is None
            for value in fields.values()
        ):
            raise ValueError("missing_or_invalid_metadata_fingerprints")
        maps.append(
            {
                key: value
                for key, value in fields.items()
                if key not in {"general.file_type", "general.quantization_version"}
            }
        )
    if maps[0] != maps[1]:
        raise ValueError("quantization_changed_non_quantization_metadata")
    return {
        "preserved_fields": len(maps[0]),
        "metadata_preserved": True,
        "lineage_authenticated": False,
        "comparison_eligible": False,
    }


def check_storage_types(rows, expected_type):
    """Check this rebuild recipe's tensor encodings, not just its file-type label."""
    matrix_types = {"BF16": {"BF16"}, "Q4_K_M": {"Q4_K", "Q6_K"}, "Q8_0": {"Q8_0"}}
    primary = {"BF16": "BF16", "Q4_K_M": "Q4_K", "Q8_0": "Q8_0"}[expected_type]
    found = set()
    for row in rows:
        rank = len(row["shape"])
        allowed = {"F32"} if rank == 1 else matrix_types[expected_type]
        if rank not in (1, 2) or row["type"] not in allowed:
            raise ValueError("qwen3_storage_type_mismatch:" + row["name"])
        found.add(row["type"])
    if primary not in found:
        raise ValueError("qwen3_primary_storage_type_missing")


def expected_tensors(config):
    if config.get("architectures") != ["Qwen3ForCausalLM"] or config.get("attention_bias"):
        raise ValueError("unsupported_qwen3_inventory_config")
    hidden = config["hidden_size"]
    head = config["head_dim"]
    query = head * config["num_attention_heads"]
    kv = head * config["num_key_value_heads"]
    middle = config["intermediate_size"]
    rows = {
        "token_embd.weight": [hidden, config["vocab_size"]],
        "output_norm.weight": [hidden],
    }
    if not config.get("tie_word_embeddings", False):
        rows["output.weight"] = [hidden, config["vocab_size"]]
    per_layer = {
        "attn_norm": [hidden],
        "ffn_norm": [hidden],
        "attn_q": [hidden, query],
        "attn_k": [hidden, kv],
        "attn_v": [hidden, kv],
        "attn_output": [query, hidden],
        "attn_q_norm": [head],
        "attn_k_norm": [head],
        "ffn_gate": [hidden, middle],
        "ffn_up": [hidden, middle],
        "ffn_down": [middle, hidden],
    }
    for layer in range(config["num_hidden_layers"]):
        for name, shape in per_layer.items():
            rows[f"blk.{layer}.{name}.weight"] = shape
    return rows


def check_inventory(inspection, config):
    expected = expected_tensors(config)
    rows = inspection["tensors"]
    actual = {row["name"]: row["shape"] for row in rows}
    if len(actual) != len(rows) or actual != expected:
        raise ValueError("qwen3_tensor_inventory_mismatch")
    for key, value in {
        "qwen3.block_count": config["num_hidden_layers"],
        "qwen3.embedding_length": config["hidden_size"],
        "qwen3.context_length": config["max_position_embeddings"],
    }.items():
        if inspection["metadata"].get(key) != value:
            raise ValueError("qwen3_config_metadata_mismatch:" + key)
    return {
        "tensor_count": len(expected),
        "tensor_names_and_shapes_match": True,
        "base_configuration_match": True,
        "numeric_equivalence_checked": False,
    }
