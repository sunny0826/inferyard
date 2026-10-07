"""Legacy synthetic packets migrate without rewriting their measurement evidence."""

import copy
import json
import shutil
from pathlib import Path

import pytest

from inferyard import SCHEMA_VERSION
from inferyard.analysis.observations import Observations, quality_observations
from inferyard.analysis.quality import summarize_quality
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.migration import migrate_run, verify_migrated_run
from inferyard.evidence.migration_transform import _score
from inferyard.evidence.storage import (
    EvidenceError,
    json_bytes,
    read_json,
    read_jsonl,
    sha256_file,
)
from inferyard.reporting.report import verify_report, write_report

ROOT = Path(__file__).resolve().parents[2]
PACKETS = ROOT / "tests/fixtures/legacy"


def _hashes(root):
    return {
        path.relative_to(root).as_posix(): sha256_file(path)
        for path in root.rglob("*")
        if path.is_file()
    }


def _source(tmp_path, version):
    return Path(shutil.copytree(PACKETS / f"synthetic-v{version}", tmp_path / "source"))


def _rehash(root, *names):
    """Reseal only a temporary synthetic mutation; never change a historical packet."""
    path = root / "manifest.json"
    manifest = read_json(path)
    for name in names:
        target = root / name
        manifest["files"][name].update(sha256=sha256_file(target), bytes=target.stat().st_size)
    path.write_bytes(json_bytes(manifest))


@pytest.mark.parametrize("version", [1, 2])
def test_migration_preserves_original_bytes_scores_and_offline_report(tmp_path, version):
    historical = PACKETS / f"synthetic-v{version}"
    historical_hashes = _hashes(historical)
    source = _source(tmp_path, version)
    before = _hashes(source)
    original_run = read_json(source / "run.json")
    original_events, _ = read_jsonl(source / "events.jsonl")
    original_scores = [event["data"] for event in original_events if event["event_type"] == "score"]
    out = tmp_path / "migrated"

    result = migrate_run(source, out)
    data = read_trial(out)
    receipt = read_json(out / "migration.json")
    assert result["schema_version"] == SCHEMA_VERSION == 3
    assert result["source_schema_version"] == version
    assert data["run"]["origin"] == "migrated"
    for key in ("run_id", "tool_version", "tool_source_sha256"):
        assert data["run"][key] == original_run[key]
    assert data["selection"]["scorer_sha256"] == original_scores[0]["scorer_sha256"]
    assert receipt["source_manifest_sha256"] == before["manifest.json"]
    assert (out / "source/manifest.json").read_bytes() == (source / "manifest.json").read_bytes()
    for name, archive in receipt["files"].items():
        original_hash = read_json(source / "manifest.json")["files"][name]["sha256"]
        assert sha256_file(out / archive["archive"]) == original_hash
    migrated_events, _ = read_jsonl(out / "events.jsonl")
    migrated_scores = [event["data"] for event in migrated_events if event["event_type"] == "score"]
    assert len(migrated_scores) == len(original_scores) == 3
    for original, migrated in zip(original_scores, migrated_scores, strict=True):
        for key in (
            "scorer_version",
            "scorer_sha256",
            "quality_state",
            "format_ok",
            "content_ok",
            "rule_results",
            "explanation",
            "reason",
        ):
            assert migrated[key] == original[key]
    assert all(event["schema_version"] == SCHEMA_VERSION for event in migrated_events)
    assert data["summary"]["counts"]["completed"] == 3
    assert (
        "migrated_evidence_does_not_grant_current_performance_qualification"
        in data["summary"]["limitations"]
    )
    assert not any(
        item["comparison_eligible"]
        for item in data["summary"]["metric_observations"]
        if item["metric_id"].startswith(("L", "C"))
    )
    verify_migrated_run(out)
    migrated_hashes = _hashes(out)
    report = tmp_path / "report"
    index = write_report([out], report)
    assert index["runs"][0]["origin"] == "migrated"
    assert verify_report(report)["semantic_verified"]
    assert _hashes(out) == migrated_hashes
    assert _hashes(source) == before
    assert _hashes(historical) == historical_hashes


@pytest.mark.parametrize("version", [1, 2])
def test_migration_rejects_changed_source_before_creating_output(tmp_path, version):
    source = _source(tmp_path, version)
    with (source / "events.jsonl").open("ab") as stream:
        stream.write(b"\n")
    out = tmp_path / "migrated"
    with pytest.raises(EvidenceError, match="migration_source_hash_mismatch"):
        migrate_run(source, out)
    assert not out.exists()


@pytest.mark.parametrize("forge_conversion_hash", [False, True])
def test_resealing_cannot_bless_changed_converted_score(tmp_path, forge_conversion_hash):
    source = _source(tmp_path, 1)
    out = tmp_path / "migrated"
    migrate_run(source, out)
    events, _ = read_jsonl(out / "events.jsonl")
    score = next(event["data"] for event in events if event["event_type"] == "score")
    score["explanation"] = "changed_after_migration"
    (out / "events.jsonl").write_bytes(b"".join(json_bytes(event) for event in events))
    names = ["events.jsonl"]
    expected = "migration_conversion_differs_from_source"
    if forge_conversion_hash:
        receipt = read_json(out / "migration.json")
        receipt["converted_files"]["events.jsonl"] = sha256_file(out / "events.jsonl")
        (out / "migration.json").write_bytes(json_bytes(receipt))
        names.append("migration.json")
        expected = "migration_conversion_inventory_mismatch"
    _rehash(out, *names)
    with pytest.raises(EvidenceError, match=expected):
        read_trial(out)


def test_resealing_cannot_bless_changed_archived_source(tmp_path):
    source = _source(tmp_path, 2)
    out = tmp_path / "migrated"
    migrate_run(source, out)
    receipt = read_json(out / "migration.json")
    name = receipt["files"]["run.json"]["archive"]
    original = read_json(out / name)
    original["tool_version"] = "changed_after_migration"
    (out / name).write_bytes(json_bytes(original))
    _rehash(out, name)
    with pytest.raises(EvidenceError, match="migration_archived_source_hash_mismatch"):
        read_trial(out)


def test_v1_rerun_keeps_historical_parent_without_inventing_parent_hash(tmp_path):
    source = _source(tmp_path, 1)
    # The old synthetic packet has no parent. Simulate an old rerun in its temporary copy.
    original = read_json(source / "run.json")
    original["parent_run_id"] = "historical-parent"
    (source / "run.json").write_bytes(json_bytes(original))
    _rehash(source, "run.json")
    out = tmp_path / "migrated"
    migrate_run(source, out)
    data = read_trial(out)
    assert data["run"]["parent_run_id"] == "historical-parent"
    assert data["run"]["relation"] == "rerun"
    assert data["selection"]["parent_events_sha256"] is None
    assert "migrated_parent_events_hash_unavailable" in data["summary"]["limitations"]
    # A newly measured run must still provide its parent evidence binding.
    data["run"]["origin"] = "measured"
    (out / "run.json").write_bytes(json_bytes(data["run"]))
    _rehash(out, "run.json")
    with pytest.raises(EvidenceError, match="migration_receipt_requires_migrated_origin"):
        read_trial(out)


@pytest.mark.parametrize("version", [True, 1.0])
@pytest.mark.parametrize(
    "filename", ["config.frozen.json", "plan.json", "events.jsonl", "memory.jsonl"]
)
def test_legacy_migration_does_not_normalize_invalid_version_types(tmp_path, filename, version):
    source = _source(tmp_path, 1)
    path = source / filename
    if filename.endswith(".jsonl"):
        records, _ = read_jsonl(path)
        assert records
        records[0]["schema_version"] = version
        path.write_bytes(b"".join(json_bytes(record) for record in records))
    else:
        original = read_json(path)
        original["schema_version"] = version
        path.write_bytes(json_bytes(original))
    _rehash(source, filename)
    out = tmp_path / "migrated"
    with pytest.raises(EvidenceError, match="legacy.*(version|binding|identity)"):
        migrate_run(source, out)
    assert not out.exists()


@pytest.mark.parametrize("answer", ['"valid JSON scalar"', "invalid JSON"])
def test_unknown_v1_json_parse_result_remains_missing_in_summary_and_observation(answer):
    case = {
        "case_id": "extraction",
        "category": "extraction",
        "rules": {
            "fields": {"name": {"type": "string", "value": "Alice"}},
            "allow_extra_fields": False,
        },
    }
    # v1 recorded only whether parsing produced an object, so both answers yielded
    # the same two rules. Their parse success cannot be recovered from those rules.
    try:
        assert not isinstance(json.loads(answer), dict)
    except json.JSONDecodeError:
        pass
    original = {
        "scorer_version": "v1",
        "scorer_sha256": "a" * 64,
        "quality_state": "fail",
        "format_ok": False,
        "content_ok": False,
        "rule_results": [
            {
                "rule": "nonempty_final_answer",
                "expected": "true",
                "observed": "true",
                "passed": True,
                "reason": "matched",
            },
            {
                "rule": "json_object",
                "expected": "true",
                "observed": "false",
                "passed": False,
                "reason": "not_matched",
            },
        ],
        "explanation": "required_rule_failed",
        "reason": None,
    }
    before = copy.deepcopy(original)
    score = _score(original, case)
    rows = [{"case_id": case["case_id"], "execution_state": "completed", "score": score}]
    quality = summarize_quality([case], rows, complete=True)
    assert original == before
    assert score["json_parse_ok"] is None
    assert quality["Q03"] == {
        "numerator": 0,
        "denominator": 1,
        "excluded": 0,
        "value": None,
        "reason": "legacy_json_parse_result_unavailable",
    }
    assert quality["Q01"]["extraction"]["rate"]["value"] == 0
    assert quality["Q04"]["value"] == 0
    assert quality["Q05"]["value"] == 0
    output = Observations(
        {
            "run_id": "legacy",
            "trial_id": "legacy-trial",
            "origin": "migrated",
            "definition_versions": {
                "scoring": "legacy-single.v1",
                "measurement": "legacy-single.v1",
            },
        },
        "legacy-workload",
        [{"path": "events.jsonl", "sha256": "a" * 64}],
        complete=True,
    )
    quality_observations(output, [case], rows, "extraction")
    metric = next(item for item in output.items if item["metric_id"] == "Q03")
    assert metric["status"] == "missing"
    assert metric["value"] is None
    assert metric["denominator"] == 1
    assert metric["missing_reason"] == "legacy_json_parse_result_unavailable"
    assert not metric["comparison_eligible"]
