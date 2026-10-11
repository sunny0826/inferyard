"""Strict old/new source-record consumption against the actual local file identity."""

import hashlib
import json
from dataclasses import asdict

import pytest

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.identity import hash_file
from inferyard.platforms.model_source import parse_metadata, source_declaration
from inferyard.platforms.model_source_budget import Budget
from tests.unit.test_model_source import FIXTURES, source


def record_and_model(tmp_path, version):
    model = tmp_path / "model.gguf"
    model.write_bytes(b"synthetic model identity")
    record = parse_metadata(source("huggingface"), (FIXTURES / "huggingface.json").read_bytes())
    record.update(
        definition=f"remote_model_source.v{version}",
        bytes=model.stat().st_size,
        sha256=hashlib.sha256(model.read_bytes()).hexdigest(),
    )
    if version == 2:
        record["acquisition_budget"] = asdict(
            Budget(
                total_seconds=14400,
                read_seconds=360,
                cleanup_seconds=120,
                max_bytes=100,
            )
        )
    return record, hash_file(model)


@pytest.mark.parametrize("version", [1, 2])
def test_original_v1_and_budget_v2_bind_without_rewriting(tmp_path, version):
    record, model = record_and_model(tmp_path, version)
    path = tmp_path / "source.json"
    original = json.dumps(record).encode()
    path.write_bytes(original)
    assert source_declaration(path, model) == (
        source("huggingface").repository,
        source("huggingface").revision,
    )
    assert path.read_bytes() == original
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


def test_large_finite_cleanup_budget_does_not_overflow_thread_wait(tmp_path):
    from inferyard.platforms.model_source_host import acquire
    from tests.unit.test_model_acquire_cli import SOURCE

    out = tmp_path / "existing"
    out.mkdir()
    with pytest.raises(PreparationError, match="^output_exists$"):
        acquire(SOURCE, out, budget=Budget(total_seconds=5, read_seconds=5, cleanup_seconds=1e308))


def test_large_finite_network_budgets_work_with_local_http(tmp_path, monkeypatch):
    from inferyard.platforms.model_source_host import acquire
    from tests.unit.test_model_acquire_cli import SOURCE
    from tests.unit.test_model_source_download import PAYLOAD
    from tests.unit.test_model_source_loopback import body_server, loopback_worker

    with body_server("trickle") as (port, _):
        loopback_worker(monkeypatch, port)
        details = acquire(
            SOURCE,
            tmp_path / "new",
            budget=Budget(total_seconds=1e308, read_seconds=1e308, cleanup_seconds=1),
        )
    assert details["bytes"] == len(PAYLOAD)
