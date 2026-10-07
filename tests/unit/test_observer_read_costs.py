import importlib
from pathlib import Path

import pytest

from inferyard.evidence.storage import EvidenceError


@pytest.fixture
def costs(monkeypatch):
    monkeypatch.syspath_prepend(str(Path(__file__).resolve().parents[2] / "scripts"))
    return importlib.import_module("observer_read_costs")


def test_nested_and_overlapping_reads_are_not_double_counted(costs):
    assert costs.union_ns([(10, 30), (12, 15), (25, 40), (40, 45)]) == 35


@pytest.mark.parametrize("pair", [(10, 9), (-1, 3), (1.0, 3), (True, 3)])
def test_invalid_clocks_are_rejected(costs, pair):
    with pytest.raises(EvidenceError, match="invalid_observer_cost_interval"):
        costs.union_ns([pair])


def test_missing_logs_remain_unknown_and_never_qualify_total_cost(costs, tmp_path):
    data = {
        "run": {"run_id": "example"},
        "requests": [{"case_id": "a", "t_send_ns": 10, "t_terminal_ns": 20}],
    }
    result = costs.audit(tmp_path, data)
    assert result["all_recorded_intervals_union_ms"] is None
    assert not result["total_observation_cost_qualified"]
    assert all(c["union_wall_ms"] is None for c in result["components"].values())
    assert result["formal_requests"][0]["recorded_wall_ms"] is None
