from copy import deepcopy

from inferyard.reporting.report_scans import scan_view


def test_actual_lengths_never_fall_back_to_targets_and_zero_is_visible():
    summary = {
        "input_lengths": {
            "bins": [
                {
                    "actual_input_tokens": None,
                    "input_target_tokens": 4096,
                    "completed_latency_p50_ms": 8,
                },
                {
                    "actual_input_tokens": 10,
                    "input_target_tokens": 12,
                    "completed_latency_p50_ms": 0,
                },
            ]
        }
    }
    before = deepcopy(summary)
    result = scan_view(summary, [])
    assert result["length_bins"][0]["point"] is None
    assert result["length_bins"][1]["point"] is not None
    assert result["input_axis_max"] == 10
    assert summary == before


def test_output_budget_never_substitutes_missing_actual_and_duplicates_are_missing():
    def metric(statistic, value):
        return dict(
            metric_id="X03", request_id="r", statistic=statistic, value=value, missing_reason=None
        )

    summary = {
        "metric_observations": [
            metric("output_budget_tokens", 512),
            metric("actual_output_tokens", 0),
        ]
    }
    requests = [
        dict(request_id="r", case_id="case", execution_state="completed", raw_finish_reason="stop")
    ]
    row = scan_view(summary, requests)["outputs"][0]
    assert row["actual"]["value"] == 0 and row["budget"]["value"] == 512
    assert row["target"]["value"] is None
    summary["metric_observations"].append(metric("actual_output_tokens", 1))
    row = scan_view(summary, requests)["outputs"][0]
    assert row["actual"] == {"value": None, "reason": "missing_or_ambiguous_observation"}


def test_position_patterns_and_denominators_are_not_merged():
    positions = {
        "family_count": 1,
        "variant_count": 2,
        "bins": [
            dict(
                family_id="one", positions=["front", "back"], correct=0, valid_executed=1, value=0
            ),
            dict(
                family_id="one",
                positions=["middle", "middle"],
                correct=1,
                valid_executed=2,
                value=0.5,
            ),
        ],
    }
    result = scan_view({"positions": positions}, [])
    assert result["positions"] == positions
    assert result["positions"] is not positions


def test_unexecuted_outputs_remain_visible_without_borrowing_counts():
    requests = [
        dict(case_id="sent", request_id="r", execution_state="completed", raw_finish_reason="stop"),
        dict(
            case_id="unmeasured",
            request_id=None,
            execution_state="not_executed",
            error_category="capacity_stop",
        ),
    ]
    summary = {
        "metric_observations": [
            dict(
                metric_id="X03",
                request_id="r",
                statistic="actual_output_tokens",
                value=8,
                missing_reason=None,
            )
        ]
    }
    view = scan_view(summary, requests)
    assert len(view["outputs"]) == 2
    assert view["outputs"][0]["actual"]["value"] == 8
    unmeasured = view["outputs"][1]
    assert unmeasured["case_id"] == "unmeasured" and unmeasured["actual"]["value"] is None
    assert unmeasured["error_category"] == "capacity_stop"
