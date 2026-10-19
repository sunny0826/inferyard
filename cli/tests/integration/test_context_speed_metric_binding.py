"""Formal metric admission against independent sealed observations."""

import hashlib
from dataclasses import replace
from pathlib import Path

import pytest

from tests.integration.context_speed_fixture import scenario, tree_hashes  # noqa: F401
from tests.integration.context_speed_fixture import speed_service as service_fixture
from tests.integration.test_context_speed_cli import invoke, run_speed

speed_service = service_fixture


@pytest.mark.parametrize("usage", [101, None, "duplicate"])
def test_formal_input_usage_must_match_tokenized_prompt(speed_service, monkeypatch, capsys, usage):
    if usage == "duplicate":
        speed_service["duplicate_usage"] = True
    else:
        speed_service["formal_usage_prompt"] = usage
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    before, calls = tree_hashes(batch), list(speed_service["calls"])
    code, response = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 0, response
    result = response["details"]
    assert result["measurement"]["valid_count"] == 0
    assert result["summary"] is None
    assert all("input_token_mismatch" in s["measurement"]["reasons"] for s in result["samples"])
    assert before == tree_hashes(batch) and calls == speed_service["calls"]


def test_verified_template_newline_survives_offline_read(speed_service, monkeypatch, capsys):
    config = speed_service["config"]
    raw = b"fixture-template\n"
    path = Path(config["model"]["template_path"])
    path.write_bytes(raw)
    config["model"]["template_sha256"] = hashlib.sha256(raw).hexdigest()
    preflight = speed_service["deps"].preflight

    def observed(value):
        identity, files = preflight(value)
        return identity, [
            replace(f, sha256=config["model"]["template_sha256"]) if f.path == str(path) else f
            for f in files
        ]

    speed_service["deps"].preflight = observed
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    before, calls = tree_hashes(batch), list(speed_service["calls"])
    path.unlink()
    code, response = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 0, response
    assert response["details"]["measurement"]["valid_count"] == 5
    assert response["details"]["summary"] is not None
    assert before == tree_hashes(batch) and calls == speed_service["calls"]
