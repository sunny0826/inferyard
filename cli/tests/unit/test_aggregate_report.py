"""Unified sealed evidence, failure accounting, reports and comparison boundaries."""

import hashlib
import json
from copy import deepcopy

import pytest

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison import compare_trials
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, read_jsonl
from inferyard.reporting.comparison_report import comparison_input
from inferyard.reporting.report import verify_report, write_report
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
    assert latency["p50"] == 2000  # The active protocol uses nearest-rank p50 in ms.
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


def test_report_autoescaping_readonly_inputs_and_output_protection(tmp_path):
    root = fixture_run(tmp_path / "runs")
    original = digest_tree(root)
    index = write_report([root], tmp_path / "report")
    summary = index["runs"][0]["summary"]
    assert summary["completeness"] == "complete"
    assert summary["quality"]["Q01"]["instruction"]["rate"]["value"] == 2 / 3
    html = (tmp_path / "report/report.html").read_text()
    assert "<script>alert" not in html and "<img src=x" not in html
    assert "&lt;script&gt;" in html and "&lt;img" in html and "{{7*7}}" in html
    assert "fetch(" not in html and "<script src=" not in html
    assert verify_report(tmp_path / "report")["verified"]
    assert original == digest_tree(root)
    with pytest.raises(FileExistsError):
        write_report([root], tmp_path / "report")
    with pytest.raises(EvidenceError, match="inside_original"):
        write_report([root], root / "new-report")


def test_missing_terminal_and_missing_score_reconstruction(tmp_path):
    root = fixture_run(tmp_path / "runs", states=["invalid", "not_executed"])
    data = read_trial(root)
    assert [r["execution_state"] for r in data["requests"]] == ["invalid", "not_executed"]
    assert data["summary"]["completeness"] == "incomplete"
    root2 = fixture_run(tmp_path / "runs", states=["completed"])
    rows, _ = read_jsonl(root2 / "events.jsonl")
    rewrite_events(root2, [r for r in rows if r["event_type"] not in ("score", "run_stopped")])
    data = read_trial(root2)
    assert data["requests"][0]["execution_state"] == "completed"
    assert data["requests"][0]["quality_state"] == "unscorable"
    quality = data["summary"]["quality"]["Q01"]["instruction"]
    assert quality["unscorable"] == 1 and quality["rate"]["denominator"] == 1
    assert quality["rate"]["value"] is None


def test_duplicate_terminal_is_rejected_even_before_manifest(tmp_path):
    root = fixture_run(tmp_path / "runs")
    rows, _ = read_jsonl(root / "events.jsonl")
    offset = next(i for i, row in enumerate(rows) if row["event_type"] == "request_finished")
    rows.insert(offset + 1, deepcopy(rows[offset]))
    rewrite_events(root, rows)
    with pytest.raises(EvidenceError, match="invalid_request_terminal"):
        read_trial(root)


def test_interrupted_request_preserves_durable_partial_answer(tmp_path):
    root = fixture_run(tmp_path / "runs", states=["invalid"])
    rows, _ = read_jsonl(root / "events.jsonl")
    partial = deepcopy(rows[0])
    partial.update(
        event_type="content",
        data={"text": "已经写入的部分答案"},
        monotonic_ns=rows[0]["monotonic_ns"] + 1,
    )
    rows.insert(1, partial)
    rewrite_events(root, rows)
    data = read_trial(root)
    assert data["requests"][0]["execution_state"] == "invalid"
    assert data["requests"][0]["content"] == "已经写入的部分答案"
    assert data["summary"]["quality"]["Q01"]["instruction"]["rate"]["value"] is None


def test_frozen_model_comparison_preserves_quality_deltas_without_performance_grant(tmp_path):
    a = fixture_run(tmp_path / "runs", comparison_mode="model")
    b = fixture_run(
        tmp_path / "runs",
        states=["completed"] * 3,
        passes=[0],
        durations=[1.5, 1.5, 3],
        model="synthetic-B",
        comparison_mode="model",
    )
    left, right = comparison_input(a)[0], comparison_input(b)[0]
    original = digest_tree(a), digest_tree(b)
    result = compare_trials(left, right, definition="phase2.v3")
    assert result["eligibility"] == {"quality": True, "completion": True, "performance": False}
    quality = next(row for row in result["quality_differences"] if row["metric_id"] == "Q01")
    assert quality["category"] == "instruction" and quality["difference"] == pytest.approx(-1 / 3)
    assert result["completion_rate_difference"] == pytest.approx(1 / 3)
    index = write_report([a, b], tmp_path / "comparison")
    assert index["comparison"] == result
    assert original == (digest_tree(a), digest_tree(b))


def test_single_default_cannot_be_promoted_to_controlled_comparison_after_measurement(tmp_path):
    data = comparison_input(fixture_run(tmp_path))[0]
    for mode in (None, "model"):
        result = compare_trials(data, deepcopy(data), mode=mode)
        assert not any(result["eligibility"].values())
        assert result["completion_rate_difference"] is None
        assert all(row["difference"] is None for row in result["quality_differences"])


@pytest.mark.parametrize(
    "section,key",
    [
        ("generation", "seed"),
        ("generation", "temperature"),
        ("generation", "top_k"),
        ("generation", "top_p"),
        ("generation", "min_p"),
        ("generation", "presence_penalty"),
        ("generation", "repeat_penalty"),
        ("generation", "max_tokens"),
        ("generation", "reasoning_mode"),
        ("generation", "stop"),
        ("generation", "seed_support"),
        ("execution", "timeout_seconds"),
        ("execution", "warmup_count"),
        ("execution", "idle_wait_seconds"),
        ("execution", "probe_prompt"),
        ("execution", "warmup_prompt"),
        ("conditions", "cache_policy"),
        ("conditions", "threads"),
        ("conditions", "threads_batch"),
        ("conditions", "context_size"),
        ("conditions", "slots"),
        ("conditions", "model_loaded"),
    ],
)
def test_frozen_recipe_mismatch_blocks_controlled_differences(tmp_path, section, key):
    left = comparable_run(tmp_path)
    right = deepcopy(left)
    value = right["config"][section][key]
    right["config"][section][key] = (
        not value
        if isinstance(value, bool)
        else value + 1
        if isinstance(value, (int, float))
        else value + ["different"]
        if isinstance(value, list)
        else value + "-different"
    )
    result = compare_trials(left, right)
    assert not any(result["eligibility"].values())
    assert result["completion_rate_difference"] is None
    assert all(row["difference"] is None for row in result["quality_differences"])


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
