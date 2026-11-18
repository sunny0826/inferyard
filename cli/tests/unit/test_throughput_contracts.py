"""Goodput thresholds and the optional throughput/interval report sections."""

from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.comparison_input import comparison_input
from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import validate_document
from inferyard.errors import ContractError
from inferyard.reporting.studio_report import build_studio_report
from tests.helpers import fixture_run
from tests.unit.phase2_contract_helpers import experiment

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]


def report(tmp_path: Path) -> JsonObject:
    root = tmp_path / "run"
    data, source = comparison_input(fixture_run(root))
    return build_studio_report([root], loaded=[(data, source)])


def test_dashboard_projects_throughput_and_pooled_intervals(tmp_path: Path) -> None:
    dashboard = report(tmp_path)["runs"][0]["dashboard"]
    assert dashboard["throughput"] == {
        "requests_per_second": pytest.approx(0.25),
        "requests_per_second_reason": None,
        "output_tokens_per_second": pytest.approx(0.5),
        "output_tokens_per_second_reason": None,
        "goodput_per_second": None,
        "goodput_per_second_reason": "thresholds_not_preregistered",
    }
    assert dashboard["output_block_intervals"] == {
        "interval_count": 0,
        "request_count": 0,
        "min": None,
        "p50": None,
        "p95": None,
        "p95_reason": "insufficient_samples",
        "max": None,
        "reason": "arrival_capture_missing",
    }


def interval_document(
    tmp_path: Path, count: int, p95: float | None, p95_reason: str | None
) -> JsonObject:
    document = report(tmp_path)
    intervals = document["runs"][0]["dashboard"]["output_block_intervals"]
    if count == 0:
        intervals.update(
            interval_count=0,
            request_count=0,
            min=None,
            p50=None,
            p95=None,
            p95_reason=p95_reason,
            max=None,
            reason="not_streaming",
        )
    else:
        intervals.update(
            interval_count=count,
            request_count=1,
            min=1,
            p50=2,
            p95=p95,
            p95_reason=p95_reason,
            max=5,
            reason=None,
        )
    return document
