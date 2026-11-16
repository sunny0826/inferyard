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
from tests.unit.test_phase2_contracts import experiment

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]


def test_goodput_thresholds_are_optional_and_frozen_into_the_plan_hash() -> None:
    baseline = compile_plan(experiment())["plan_sha256"]
    source = experiment()
    source["goodput_thresholds"] = {"quality_state": "pass", "e2e_latency_ms_max": 500}
    plan = compile_plan(source)
    assert plan["experiment"]["goodput_thresholds"] == source["goodput_thresholds"]
    assert plan["plan_sha256"] != baseline
    assert compile_plan(source) == plan
    source["goodput_thresholds"]["e2e_latency_ms_max"] = 600
    assert compile_plan(source)["plan_sha256"] != plan["plan_sha256"]


@pytest.mark.parametrize(
    "thresholds",
    [
        {"quality_state": "fail", "e2e_latency_ms_max": 500},
        {"quality_state": "pass", "e2e_latency_ms_max": 0},
        {"quality_state": "pass", "e2e_latency_ms_max": -1},
        {"quality_state": "pass", "e2e_latency_ms_max": float("inf")},
        {"quality_state": "pass"},
        {"e2e_latency_ms_max": 500},
        {"quality_state": "pass", "e2e_latency_ms_max": 500, "extra": 1},
    ],
)
def test_invalid_goodput_thresholds_are_rejected_by_plan_validation(
    thresholds: JsonObject,
) -> None:
    source = experiment()
    source["goodput_thresholds"] = thresholds
    with pytest.raises(ContractError):
        compile_plan(source)


def report(tmp_path: Path) -> JsonObject:
    root = tmp_path / "run"
    data, source = comparison_input(fixture_run(root))
    return build_studio_report([root], loaded=[(data, source)])


def test_fixture_trial_emits_the_n_groups_after_catalogue_load(tmp_path: Path) -> None:
    from inferyard.registry import catalogue

    metrics = {item["metric_id"]: item for item in catalogue("metrics")["items"]}
    assert len(metrics) == 46
    assert metrics["N04"]["required_capabilities"] == ["endpoint_usage_batch_window"]

    data, _ = comparison_input(fixture_run(tmp_path / "run"))
    observations = data["summary"]["metric_observations"]
    assert {"N01", "N02", "N03", "N04"} <= {item["metric_id"] for item in observations}
    itl = [item for item in observations if item["metric_id"] == "N03"]
    assert sum(item["statistic"] == "token_itl" for item in itl) == 2
    assert any(item["statistic"] == "service_queue_delay" for item in itl)
    pooled = {
        item["statistic"]
        for item in observations
        if item["metric_id"] == "L05" and item["statistic"].startswith("pooled_")
    }
    assert pooled == {"pooled_min", "pooled_p50", "pooled_p95", "pooled_max"}


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


def test_old_reports_without_the_optional_sections_remain_readable(tmp_path: Path) -> None:
    document = report(tmp_path)
    for key in ("throughput", "output_block_intervals"):
        document["runs"][0]["dashboard"].pop(key)
    validate_document("studio_report", document)


def test_half_written_throughput_is_rejected(tmp_path: Path) -> None:
    document = report(tmp_path)
    throughput = document["runs"][0]["dashboard"]["throughput"]
    throughput["requests_per_second"] = None
    with pytest.raises(ContractError):
        validate_document("studio_report", document)

    document = report(tmp_path)
    del document["runs"][0]["dashboard"]["throughput"]["goodput_per_second_reason"]
    with pytest.raises(ContractError):
        validate_document("studio_report", document)


def test_unknown_interval_fields_are_rejected(tmp_path: Path) -> None:
    document = report(tmp_path)
    document["runs"][0]["dashboard"]["output_block_intervals"]["excluded_interval_count"] = 0
    with pytest.raises(ContractError):
        validate_document("studio_report", document)


def test_nonempty_intervals_require_ordered_quantiles_and_a_sample_bounded_p95(
    tmp_path: Path,
) -> None:
    document = report(tmp_path)
    intervals = document["runs"][0]["dashboard"]["output_block_intervals"]
    intervals.update(
        interval_count=5,
        request_count=2,
        min=1,
        p50=2,
        p95=None,
        p95_reason="insufficient_samples",
        max=5,
        reason=None,
    )
    validate_document("studio_report", document)

    document = report(tmp_path)
    intervals = document["runs"][0]["dashboard"]["output_block_intervals"]
    intervals.update(
        interval_count=5,
        request_count=2,
        min=1,
        p50=2,
        p95=3,
        p95_reason=None,
        max=5,
        reason=None,
    )
    with pytest.raises(ContractError):
        validate_document("studio_report", document)

    document = report(tmp_path)
    intervals = document["runs"][0]["dashboard"]["output_block_intervals"]
    intervals.update(
        interval_count=25,
        request_count=2,
        min=1,
        p50=2,
        p95=0.5,
        p95_reason=None,
        max=5,
        reason=None,
    )
    with pytest.raises(ContractError):
        validate_document("studio_report", document)


def test_zero_intervals_keep_quantiles_null_and_a_reason(tmp_path: Path) -> None:
    document = report(tmp_path)
    intervals = document["runs"][0]["dashboard"]["output_block_intervals"]
    intervals["min"] = 1
    with pytest.raises(ContractError):
        validate_document("studio_report", document)

    document = report(tmp_path)
    intervals = document["runs"][0]["dashboard"]["output_block_intervals"]
    intervals["reason"] = None
    with pytest.raises(ContractError):
        validate_document("studio_report", document)


def test_interval_reason_and_p95_reason_are_independent(tmp_path: Path) -> None:
    document = deepcopy(report(tmp_path))
    intervals = document["runs"][0]["dashboard"]["output_block_intervals"]
    intervals.update(interval_count=0, reason="not_streaming")
    validate_document("studio_report", document)


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


@pytest.mark.parametrize(
    "count,p95,p95_reason",
    [
        (0, None, "insufficient_samples"),
        (1, None, "insufficient_samples"),
        (19, None, "insufficient_samples"),
        (20, 4.0, None),
    ],
)
def test_interval_p95_rule_accepts_the_unified_sample_boundaries(
    tmp_path: Path, count: int, p95: float | None, p95_reason: str | None
) -> None:
    validate_document("studio_report", interval_document(tmp_path, count, p95, p95_reason))


@pytest.mark.parametrize(
    "count,p95,p95_reason",
    [
        (0, None, "arbitrary"),
        (1, None, "arbitrary"),
        (19, None, None),
        (20, None, "insufficient_samples"),
    ],
)
def test_interval_p95_rule_rejects_wrong_reason_or_missing_tail(
    tmp_path: Path, count: int, p95: float | None, p95_reason: str | None
) -> None:
    document = interval_document(tmp_path, count, p95, p95_reason)
    with pytest.raises(ContractError):
        validate_document("studio_report", document)
