from inferyard.analysis.length_resources import resource_cohort


def metric(request, value, source="sensor-A", reason=None):
    return {
        "request_id": request,
        "metric_id": "C07",
        "definition_version": "phase2.v1",
        "statistic": "observed_max",
        "unit": "celsius",
        "layer": "system",
        "source": source,
        "value": value,
        "missing_reason": reason,
        "limitations": [],
    }


def test_resource_cohort_keeps_failures_missing_and_sources_separate():
    requests = [
        {"request_id": "a", "execution_state": "completed"},
        {"request_id": "b", "execution_state": "failed"},
        {"request_id": "c", "execution_state": "completed"},
        {"request_id": "d", "execution_state": "invalid"},
    ]
    result = resource_cohort(
        requests,
        [
            metric("a", 0),
            metric("b", 20),
            metric("c", None, reason="read_failed"),
            metric("d", 999),
            metric("a", 100, source="sensor-B"),
        ],
    )
    a, b = result["series"]
    assert a["valid_executed"] == 3 and a["observed_requests"] == 2
    assert (a["min"], a["p50"], a["max"]) == (0, 0, 20)
    assert a["missing_reasons"] == {"read_failed": 1}
    assert b["p50"] == 100 and b["missing_requests"] == 2
    assert b["missing_reasons"] == {"missing_observation": 2}


def test_ambiguous_observations_do_not_duplicate_request_or_choose_one():
    requests = [{"request_id": "a", "execution_state": "completed"}]
    result = resource_cohort(requests, [metric("a", 10), metric("a", 30)])
    row = result["series"][0]
    assert row["p50"] is None and row["observed_requests"] == 0
    assert row["missing_reasons"] == {"ambiguous_observation": 1}
    assert resource_cohort(requests, [])["status"] == "missing"
