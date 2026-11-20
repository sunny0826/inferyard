"""Unified sealed evidence, failure accounting, reports and comparison boundaries."""

import hashlib
import json
from copy import deepcopy

import pytest

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison import compare_trials
from inferyard.errors import EvidenceError
from inferyard.analysis.ledger import read_trial
from inferyard.evidence.storage import read_jsonl
from inferyard.analysis.comparison_input import comparison_input
from inferyard.reporting.comparison_report import build_comparison
from inferyard.reporting.studio_report import write_studio_report
from tests.helpers import fixture_run


def digest_tree(root):
    return {
        str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in root.rglob("*")
        if path.is_file()
    }


def rewrite_events(root, rows):
    """Simulate an unsealed crash log without leaving unrelated sequence faults."""
    (root / "manifest.json").unlink()
    for seq, row in enumerate(rows, 1):
        row["seq"] = seq
    (root / "events.jsonl").write_text(
        "".join(json.dumps(row, ensure_ascii=False) + "\n" for row in rows)
    )


def comparable_run(root, **kwargs):
    return comparison_input(fixture_run(root, comparison_mode="model", **kwargs))[0]


def test_exact_ledger_preserves_failures_and_uses_active_latency_definition(tmp_path):
    root = fixture_run(
        tmp_path,
        states=["completed"] * 4 + ["failed"] * 3,
        passes=[0, 2],
        durations=[1, 2, 3, 4, 100, 100, 100],
        budget_exhausted=[2, 3],
    )
    data = read_trial(root)
    summary = data["summary"]
    assert data["run"]["schema_version"] == SCHEMA_VERSION == 3
    assert data["run"]["execution_mode"] == "single"
    assert summary["counts"]["valid_executed"] == 7
    assert summary["counts"]["budget_exhausted_completed"] == 2
    assert summary["completion_rate"]["value"] == 4 / 7
    quality = summary["quality"]["Q01"]["instruction"]
    assert quality["rate"]["numerator"] == 2
    assert quality["rate"]["denominator"] == 7
    assert quality["rate"]["value"] == 2 / 7
    latency = summary["performance"]["instruction"]["metrics"]["L03"]
    assert latency["sample_count"] == 4 and latency["excluded"] == 3
    assert latency["p50"] == 2000
    assert latency["max"] == 4000
    assert latency["quantile_method"] == "nearest_rank.phase2.v1"
    assert summary["completeness"] == "complete"


def test_five_states_remain_distinct_and_incomplete_quality_has_no_rate(tmp_path):
    states = ["completed", "failed", "cancelled", "invalid", "not_executed"]
    data = read_trial(fixture_run(tmp_path, states=states))
    assert [row["execution_state"] for row in data["requests"]] == states
    counts = data["summary"]["counts"]
    assert counts["planned"] == sum(counts[state] for state in states) == 5
    assert counts["executed"] == 4 and counts["valid_executed"] == 2
    quality = data["summary"]["quality"]["Q01"]["instruction"]
    assert quality["rate"]["denominator"] == 2 and quality["rate"]["excluded"] == 3
    assert quality["rate"]["value"] is None
    assert data["summary"]["stop_reason"] == "tool_interrupted"
    assert data["summary"]["completeness"] == "incomplete"


@pytest.mark.parametrize(
    "path",
    [
        ("identity", "verification"),
        ("config", "device", "id"),
        ("config", "engine", "adapter"),
        ("config", "engine", "release"),
        ("config", "engine", "binary_sha256"),
    ],
)
def test_identically_unknown_identity_never_becomes_comparable(tmp_path, path):
    left = comparable_run(tmp_path)
    target = left
    for key in path[:-1]:
        target = target[key]
    target[path[-1]] = "unknown"
    result = compare_trials(left, deepcopy(left))
    assert not any(result["eligibility"].values())
    assert result["completion_rate_difference"] is None
