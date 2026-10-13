from copy import deepcopy

import pytest

from inferyard.analysis.public_metrics import project_metrics
from inferyard.analysis.public_metrics_check import validate_metrics
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.reporting.public_package import number


def test_redaction_preserves_distinct_missing_reasons_sources_zero_and_counts():
    row = {
        "metric_id": "C07",
        "definition_version": "phase2.v1",
        "statistic": "observed_max",
        "unit": "celsius",
        "layer": "system",
        "source": "/SECRET/sensor-A",
        "status": "missing",
        "missing_reason": "SECRET-private-reason-A",
        "value": None,
        "sample_count": 0,
        "numerator": None,
        "denominator": 4,
        "excluded": 4,
        "coverage_ratio": None,
        "request_id": "SECRET-request",
        "group": {
            "workload_id": "SECRET-workload",
            "category": "math",
            "error_category": "SECRET-error",
        },
        "limitations": ["SECRET-limitation"],
        "comparison_eligible": False,
        "sampled_start_ns": 100,
        "sampled_end_ns": 130,
        "evidence_refs": [{"path": "/SECRET/evidence"}],
    }
    other = deepcopy(row)
    other.update(source="/SECRET/sensor-B", missing_reason="SECRET-private-reason-B")
    zero = deepcopy(row)
    zero.update(value=0, status="derived", missing_reason=None, excluded=0)
    result = project_metrics([row, other, zero], number)
    assert b"SECRET" not in json_bytes(result)
    a, b, c = result["observations"]
    assert a["source"]["redacted"] and a["source"]["value"] is None
    assert a["source"]["sha256"] != b["source"]["sha256"]
    assert a["missing_reason"]["sha256"] != b["missing_reason"]["sha256"]
    assert a["value"] is None and c["value"] == 0
    assert c["missing_reason"] is None
    assert a["denominator"] == 4 and a["excluded"] == 4
    assert a["sampled_span_ns"] == 30
    assert "sampled_start_ns" not in a and "evidence_refs" not in a
    assert a["request_identity_sha256"] == b["request_identity_sha256"]

    validate_metrics(result)
    mutations = [
        ("observation_count", 2),
        ("value", 0),
        ("denominator", -1),
        ("public_comparison_authorized", True),
        ("status", {"value": "derived", "sha256": "0" * 64, "redacted": False}),
        ("source", {"value": "SECRET-source", "sha256": "0" * 64, "redacted": False}),
    ]
    for key, value in mutations:
        altered = deepcopy(result)
        if key == "observation_count":
            altered[key] = value
        else:
            altered["observations"][0][key] = value
        with pytest.raises(EvidenceError, match="metrics_inconsistent"):
            validate_metrics(altered)
