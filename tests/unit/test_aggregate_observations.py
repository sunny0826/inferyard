from copy import deepcopy

import pytest

from inferyard.contracts.validation import ContractError, validate_document
from tests.unit.test_phase2_contracts import observation


def aggregate():
    metric = observation()
    metric.update(
        run_id=None,
        trial_id=None,
        request_id=None,
        analysis_id="analysis-1",
        source_run_ids=["r1", "r2"],
    )
    return metric


def analysis():
    return {
        "schema_version": 3,
        "analysis_id": "analysis-1",
        "source_runs": [{"run_id": rid, "manifest_sha256": "a" * 64} for rid in ("r1", "r2")],
        "parent_analysis_id": None,
        "reason": "repeat",
        "definition_versions": {"measurement": "v2", "scoring": "v1", "comparison": "v2"},
        "scorer_id": "scorer-1",
        "scorer_sha256": "b" * 64,
        "answer_policy_sha256": "c" * 64,
        "metrics": [aggregate()],
        "limitations": [],
    }


def test_aggregate_has_explicit_sources_without_single_trial_attribution():
    validate_document("metric_observation", aggregate())
    validate_document("analysis", analysis())
    validate_document("metric_observation", observation())  # existing v2 remains accepted


@pytest.mark.parametrize(
    "change",
    [
        {"source_run_ids": ["r1", "r1"]},
        {"source_run_ids": []},
        {"run_id": "r1"},
        {"sampled_start_ns": 1, "sampled_end_ns": 2},
        {"request_id": "request-1"},
    ],
)
def test_invalid_aggregate_identity_or_clock_rejected(change):
    with pytest.raises(ContractError):
        validate_document("metric_observation", {**aggregate(), **change})


@pytest.mark.parametrize("change", [{"analysis_id": "other"}, {"source_run_ids": ["foreign"]}])
def test_parent_analysis_requires_known_aggregate_sources(change):
    data = deepcopy(analysis())
    data["metrics"][0].update(change)
    with pytest.raises(ContractError, match="binding"):
        validate_document("analysis", data)


def test_full_repeat_group_builds_typed_numeric_analyses(tmp_path):
    from inferyard.analysis.repetition_analysis import make_analysis
    from inferyard.analysis.repetition_metrics import repetition_metrics
    from tests.unit.test_repetition_metrics import inputs

    plan, runs = inputs()
    for i, data in enumerate(runs):
        path = tmp_path / str(i)
        path.mkdir()
        (path / "manifest.json").write_text("{}")
        data.update(
            path=path,
            selection={"scorer_sha256": "a" * 64},
            bundle={"answer_policy": {"normalization": "test"}},
        )
    group = repetition_metrics(plan, runs)[0]
    result = make_analysis(
        group,
        runs,
        dict.fromkeys(("measurement", "scoring", "comparison"), "phase2.v1"),
        [{"path": "repetition-summary.json", "sha256": "b" * 64}],
    )
    validate_document("analysis", result)
    quality = next(m for m in result["metrics"] if m["metric_id"] == "S03")
    assert quality["value"] == 0.75 and quality["denominator"] == 40
    assert quality["run_id"] is None and len(quality["source_run_ids"]) == 3
    spread = next(m for m in result["metrics"] if m["statistic"] == "trial_median_range")
    assert spread["value"] == 2 and spread["unit"] == "ms"
