"""Physical originals and incomplete measurements, using the public command."""

import shutil
from pathlib import Path

import pytest

from tests.integration.context_speed_fixture import scenario, tree_hashes  # noqa: F401
from tests.integration.context_speed_fixture import speed_service as service_fixture
from tests.integration.test_context_speed_cli import invoke, run_speed

speed_service = service_fixture


@pytest.mark.parametrize("mode", ["missing", "counts", "cached", "short"])
def test_measurement_gaps(speed_service, monkeypatch, capsys, mode):
    if mode == "missing":
        speed_service["timing"] = False
    elif mode == "counts":
        speed_service["formal_tokens"] = [100, 101, 102, 103, 104]
    elif mode == "cached":
        speed_service["custom_timing"] = dict(
            cache_n=20, prompt_n=80, prompt_ms=400, predicted_n=11, predicted_ms=500
        )
    else:
        speed_service["formal_output"] = 11
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    before, calls = tree_hashes(batch), list(speed_service["calls"])
    code, result = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 0, result
    d = result["details"]
    assert d["summary"] is None
    assert d["measurement"]["status"] == "incomplete"
    if mode == "counts":
        assert [s["columns"]["prompt_length"]["value"] for s in d["samples"]] == [
            100,
            101,
            102,
            103,
            104,
        ]
        assert d["measurement"]["valid_count"] == 5
    elif mode == "cached":
        s = d["samples"][0]
        assert [s["columns"][k]["value"] for k in ("time", "prefill", "decode")] == [0.4, 200, 20]
        assert "cold_cache_unproven" in s["measurement"]["reasons"]
    else:
        assert d["measurement"]["valid_count"] == 0
    assert before == tree_hashes(batch) and calls == speed_service["calls"]
