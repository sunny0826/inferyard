import importlib.util
from pathlib import Path

import pytest

SPEC = importlib.util.spec_from_file_location(
    "qwen3_inventory", Path(__file__).parents[2] / "scripts/qwen3_tensor_inventory.py"
)
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


@pytest.fixture
def config():
    return dict(
        architectures=["Qwen3ForCausalLM"],
        attention_bias=False,
        hidden_size=8,
        head_dim=4,
        num_attention_heads=4,
        num_key_value_heads=2,
        intermediate_size=12,
        vocab_size=20,
        tie_word_embeddings=True,
        num_hidden_layers=2,
        max_position_embeddings=32,
    )


def test_head_dimensions_and_tied_output(config):
    expected = module.expected_tensors(config)
    assert len(expected) == 24
    assert expected["blk.1.attn_q.weight"] == [8, 16]
    assert expected["blk.1.attn_output.weight"] == [16, 8]
    assert expected["blk.1.attn_k.weight"] == [8, 8]
    assert "output.weight" not in expected
    config["tie_word_embeddings"] = False
    assert module.expected_tensors(config)["output.weight"] == [8, 20]


@pytest.mark.parametrize("mutation", [None, "missing", "shape", "duplicate", "metadata"])
def test_inventory_rejects_partial_or_wrong_model(config, mutation):
    rows = [
        {"name": name, "shape": shape} for name, shape in module.expected_tensors(config).items()
    ]
    data = {
        "tensors": rows,
        "metadata": {
            "qwen3.block_count": 2,
            "qwen3.embedding_length": 8,
            "qwen3.context_length": 32,
        },
    }
    if mutation == "missing":
        rows.pop()
    elif mutation == "shape":
        rows[0]["shape"] = [20, 8]
    elif mutation == "duplicate":
        rows.append(rows[0])
    elif mutation == "metadata":
        data["metadata"]["qwen3.block_count"] = 3
    if mutation:
        with pytest.raises(ValueError):
            module.check_inventory(data, config)
    else:
        assert module.check_inventory(data, config)["tensor_count"] == 24


@pytest.mark.parametrize("kind,storage", [("BF16", "BF16"), ("Q4_K_M", "Q4_K"), ("Q8_0", "Q8_0")])
def test_actual_storage_matches_recipe(kind, storage):
    rows = [
        {"name": "norm", "shape": [4], "type": "F32"},
        {"name": "matrix", "shape": [4, 4], "type": storage},
    ]
    module.check_storage_types(rows, kind)
    rows[1]["type"] = "F32"
    with pytest.raises(ValueError, match="storage_type_mismatch"):
        module.check_storage_types(rows, kind)


def test_q4_mixture_requires_q4_tensor():
    rows = [{"name": "matrix", "shape": [256, 256], "type": "Q6_K"}]
    with pytest.raises(ValueError, match="primary_storage_type_missing"):
        module.check_storage_types(rows, "Q4_K_M")
    rows.append({"name": "other", "shape": [256, 256], "type": "Q4_K"})
    module.check_storage_types(rows, "Q4_K_M")


@pytest.mark.parametrize("change", [None, "template", "unknown_field", "missing", "bad_hash"])
def test_metadata_only_allows_quantization_fields_to_change(change):
    fields = dict.fromkeys(
        [
            "general.architecture",
            "general.file_type",
            "tokenizer.ggml.tokens",
            "tokenizer.chat_template",
        ],
        "a" * 64,
    )
    other = {**fields, "general.file_type": "b" * 64, "general.quantization_version": "c" * 64}
    if change == "template":
        other["tokenizer.chat_template"] = "d" * 64
    elif change == "unknown_field":
        other["unexpected.config"] = "d" * 64
    elif change == "missing":
        del other["tokenizer.ggml.tokens"]
    elif change == "bad_hash":
        other["tokenizer.ggml.tokens"] = "unknown"
    args = ({"metadata_field_sha256": fields}, {"metadata_field_sha256": other})
    if change:
        with pytest.raises(ValueError):
            module.check_preserved_metadata(*args)
    else:
        result = module.check_preserved_metadata(*args)
        assert result["metadata_preserved"] is True
        assert result["comparison_eligible"] is False
