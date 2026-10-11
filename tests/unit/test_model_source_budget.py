"""Operator budgets and versioned source records, using only synthetic bytes."""

import json
from dataclasses import asdict
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.config.preparation_io import PreparationError
from inferyard.platforms import model_source_host as host
from inferyard.platforms.identity import hash_file
from inferyard.platforms.model_source import source_declaration
from tests.unit.test_model_acquire_cli import SOURCE
from tests.unit.test_model_source import source
from tests.unit.test_model_source_download import PAYLOAD, fixture_client


def test_extended_budget_reaches_acquisition_from_cli(tmp_path, monkeypatch, capsys):
    seen = []

    def acquire(url, out, token_env, *, budget):
        seen.append(budget)
        return {"ready_to_run": False, "model_requests_sent": 0}

    monkeypatch.setattr(host, "acquire", acquire)
    assert (
        main(
            [
                "model",
                "acquire",
                "--source",
                SOURCE,
                "--out",
                str(tmp_path / "new"),
                "--timeout-seconds",
                "14400",
                "--read-timeout-seconds",
                "300",
                "--cleanup-timeout-seconds",
                "120",
            ]
        )
        == 0
    )
    assert asdict(seen[0]) == dict(
        total_seconds=14400, read_seconds=300, cleanup_seconds=120, max_bytes=20 * 1024**3
    )
    assert json.loads(capsys.readouterr().out)["status"] == "prepared"


@pytest.mark.parametrize("option", ["timeout", "read-timeout", "cleanup-timeout"])
@pytest.mark.parametrize("value", ["0", "-1", "nan", "inf"])
def test_invalid_cli_budget_rejected_before_child(option, value, tmp_path, monkeypatch, capsys):
    monkeypatch.setattr(host.subprocess, "Popen", lambda *a, **k: pytest.fail("child started"))
    assert (
        main(
            [
                "model",
                "acquire",
                "--source",
                SOURCE,
                "--out",
                str(tmp_path / "new"),
                f"--{option}-seconds={value}",
            ]
        )
        == 2
    )
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_model_source"]
    assert not (tmp_path / "new").exists()


def test_acquired_record_preserves_actual_budget_and_binds(tmp_path):
    from inferyard.platforms.model_source_download import transfer

    budget = host.Budget(total_seconds=14400, read_seconds=300, cleanup_seconds=120)
    client, _ = fixture_client()
    with client:
        details = transfer(source("huggingface"), tmp_path / "new", None, budget, client=client)
    record = json.loads((tmp_path / "new/source.json").read_bytes())
    assert record["definition"] == "remote_model_source.v2"
    assert record["acquisition_budget"] == asdict(budget)
    assert source_declaration(details["source_record"], hash_file(Path(details["model"]))) == (
        source("huggingface").repository,
        source("huggingface").revision,
    )


@pytest.mark.parametrize("change", ["missing", "extra", "bool", "negative", "unknown", "v1-budget"])
def test_invalid_versioned_budgets_reject_without_rewriting(tmp_path, change):
    from tests.unit.test_model_source_candidates import record_for

    model = tmp_path / "model.gguf"
    model.write_bytes(PAYLOAD)
    path = record_for(model, tmp_path / "source.json")
    record = json.loads(path.read_bytes())
    record.update(definition="remote_model_source.v2", acquisition_budget=asdict(host.Budget()))
    if change == "missing":
        record.pop("acquisition_budget")
    elif change == "extra":
        record["acquisition_budget"]["extra"] = 1
    elif change == "bool":
        record["acquisition_budget"]["read_seconds"] = True
    elif change == "negative":
        record["acquisition_budget"]["total_seconds"] = -1
    elif change == "unknown":
        record["definition"] = "remote_model_source.v999"
    else:
        record["definition"] = "remote_model_source.v1"
    path.write_text(json.dumps(record))
    original = path.read_bytes()
    with pytest.raises(PreparationError, match="model_source_metadata_mismatch"):
        source_declaration(path, hash_file(model))
    assert path.read_bytes() == original
