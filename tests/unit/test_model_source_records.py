"""Strict old/new source-record consumption against the actual local file identity."""

import json

import pytest

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.identity import hash_file
from inferyard.platforms.model_source import source_declaration
from tests.unit.test_model_source import record_for, source


def record_and_model(tmp_path, version):
    model = tmp_path / "model.gguf"
    model.write_bytes(b"synthetic model identity")
    return record_for(model, version), hash_file(model)


@pytest.mark.parametrize("version", [1, 2, 3])
def test_original_and_current_records_bind_without_rewriting(tmp_path, version):
    record, model = record_and_model(tmp_path, version)
    path = tmp_path / "source.json"
    original = json.dumps(record).encode()
    path.write_bytes(original)
    assert source_declaration(path, model) == (
        source("huggingface").repository,
        source("huggingface").revision,
    )
    assert path.read_bytes() == original
    if version < 3:
        assert "metadata_sha256" in json.loads(path.read_bytes())
    if version == 1:
        assert "acquisition_budget" not in json.loads(path.read_bytes())


@pytest.mark.parametrize(
    "change",
    [
        "missing_budget_key",
        "extra_record_key",
        "budget_wrong_shape",
        "bytes_exceed_budget",
        "max_bytes_bool",
        "max_bytes_fractional",
        "max_bytes_over_cap",
        "huge_integer",
    ],
)
def test_record_budget_shape_and_values_are_strict(tmp_path, change):
    record, model = record_and_model(tmp_path, 2)
    budget = record["acquisition_budget"]
    if change == "missing_budget_key":
        del budget["cleanup_seconds"]
    elif change == "extra_record_key":
        record["SECRET"] = "SECRET"
    elif change == "budget_wrong_shape":
        record["acquisition_budget"] = []
    else:
        key, value = {
            "bytes_exceed_budget": ("max_bytes", 1),
            "max_bytes_bool": ("max_bytes", True),
            "max_bytes_fractional": ("max_bytes", 30.5),
            "max_bytes_over_cap": ("max_bytes", 21 * 1024**3),
            "huge_integer": ("total_seconds", 10**400),
        }[change]
        budget[key] = value
    path = tmp_path / "source.json"
    path.write_text(json.dumps(record))
    with pytest.raises(PreparationError, match="^model_source_metadata_mismatch$"):
        source_declaration(path, model)


@pytest.mark.parametrize("field", ["total_seconds", "read_seconds", "cleanup_seconds"])
@pytest.mark.parametrize("value", [0, -1, True, float("nan"), float("inf"), "180", None])
def test_record_timeouts_reject_invalid_numbers(tmp_path, field, value):
    record, model = record_and_model(tmp_path, 2)
    record["acquisition_budget"][field] = value
    path = tmp_path / "source.json"
    path.write_text(json.dumps(record))
    with pytest.raises(PreparationError, match="^model_source_metadata_mismatch$"):
        source_declaration(path, model)


@pytest.mark.parametrize(
    "change", ["missing_metadata_sha256", "bad_metadata_sha256", "v3_with_metadata_sha256"]
)
def test_legacy_metadata_hash_field_stays_strict(tmp_path, change):
    version = 1 if change != "v3_with_metadata_sha256" else 3
    record, model = record_and_model(tmp_path, version)
    if change == "missing_metadata_sha256":
        del record["metadata_sha256"]
    elif change == "bad_metadata_sha256":
        record["metadata_sha256"] = "not-a-hash"
    else:
        record["metadata_sha256"] = "0" * 64
    path = tmp_path / "source.json"
    path.write_text(json.dumps(record))
    with pytest.raises(PreparationError, match="^model_source_metadata_mismatch$"):
        source_declaration(path, model)


@pytest.mark.parametrize("definition", ["remote_model_source.v0", "remote_model_source.v4"])
def test_unknown_definitions_are_rejected(tmp_path, definition):
    record, model = record_and_model(tmp_path, 3)
    record["definition"] = definition
    path = tmp_path / "source.json"
    path.write_text(json.dumps(record))
    with pytest.raises(PreparationError, match="^model_source_metadata_mismatch$"):
        source_declaration(path, model)
