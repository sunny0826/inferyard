"""Studio report is one projected JSON document; it never renders or verifies evidence."""

import json
import shutil
from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard.contracts.schemas import export_schema
from inferyard.contracts.validation import validate_document
from inferyard.errors import ContractError, EvidenceError
from inferyard.analysis.comparison_input import comparison_input
from inferyard.reporting.studio_report import (
    build_studio_report,
    read_studio_report,
    write_studio_report,
)
from tests.helpers import fixture_run

CATEGORIES = ("instruction", "extraction", "qa", "math", "classification", "structured")


def loaded(root, **kwargs):
    return comparison_input(fixture_run(root, **kwargs))


def build(tmp_path, data, source):
    return build_studio_report([tmp_path / "run"], loaded=[(data, source)])


def test_builds_one_studio_document_from_a_run(tmp_path):
    data, source = loaded(tmp_path / "run")
    report = build_studio_report([tmp_path / "run"], loaded=[(data, source)])

    assert set(report) == {"schema_version", "kind", "runs"}
    assert report["schema_version"] == 3
    assert report["kind"] == "studio-report.v1"
    (run,) = report["runs"]
    assert run["source"] == {"run_id": source["run_id"], "path": source["path"]}
    assert run["model"] == data["config"]["model"]["display_name"]
    assert run["threads"] == 6
    assert type(run["threads"]) is int
    assert run["diagnostic"] is False
    assert run["facets"] == {
        "bundle_label": f"{data['bundle']['bundle_id']} · {data['bundle']['version']}"
    }
    assert run["profile"]["title"] == "synthetic-A"
    assert run["profile"]["engine"]["adapter"] == "prism_llama_server_v1"
    assert run["profile"]["engine"]["backend"] == "cpu"
    assert run["profile"]["engine"]["binary_path"].endswith("fixture-server")
    assert run["profile"]["machine"] == {
        "cpu": "fixture-cpu",
        "logical_cpus": 12,
        "memory_total_bytes": 40 * 1024**3,
    }
    assert type(run["profile"]["machine"]["memory_total_bytes"]) is int
    assert run["summary"] == {
        "completion_rate": {"numerator": 2, "denominator": 3, "excluded": 0},
        "stop_reason": "plan_finished",
    }
    assert [row["case_id"] for row in run["requests"]] == ["case-0", "case-1", "case-2"]
    assert [row["state"] for row in run["requests"]] == ["completed", "completed", "failed"]
    assert [row["quality"] for row in run["requests"]] == ["pass", "pass", None]
    assert [row["latency_ms"] for row in run["requests"]] == [1000.0, 2000.0, 3000.0]
    assert run["requests"][2]["category_label"] == "指令遵循"
    assert run["dashboard"]["latency"] == {
        "sample_count": 2,
        "excluded": 1,
        "min": 1000.0,
        "p50": 1000.0,
        "p95": None,
        "max": 2000.0,
    }
    assert run["dashboard"]["quality"] == {
        "numerator": 2,
        "denominator": 3,
        "excluded": 0,
        "complete": True,
        "reason": None,
    }
    assert [row["id"] for row in run["dashboard"]["categories"]] == ["instruction"]


def test_multiple_runs_keep_their_own_labels_and_duplicates_are_rejected(tmp_path):
    first, second = tmp_path / "first", tmp_path / "second"
    one, two = loaded(first), loaded(second, model="synthetic-B")
    report = build_studio_report([first, second], loaded=[one, two])
    assert [run["model"] for run in report["runs"]] == ["synthetic-A", "synthetic-B"]

    copy = tmp_path / "copy"
    shutil.copytree(one[1]["path"], copy)
    duplicate = comparison_input(copy)
    assert duplicate[1]["run_id"] == one[1]["run_id"]
    with pytest.raises(EvidenceError, match="duplicate_report_run"):
        build_studio_report([first, copy], loaded=[one, duplicate])


def test_run_count_is_bounded_and_ids_are_unique(tmp_path):
    data, source = loaded(tmp_path / "run")
    report = build(tmp_path, data, source)
    base = report["runs"][0]

    def document(count):
        runs = []
        for index in range(count):
            run = deepcopy(base)
            run["source"] = {"run_id": f"run-{index}", "path": f"/synthetic/{index}"}
            runs.append(run)
        return {**report, "runs": runs}

    validate_document("studio_report", document(1))
    validate_document("studio_report", document(100))
    with pytest.raises(ContractError, match="too many items"):
        validate_document("studio_report", document(101))
    empty = {**report, "runs": []}
    with pytest.raises(ContractError, match="too few items"):
        validate_document("studio_report", empty)
    repeated = document(2)
    repeated["runs"][1]["source"]["run_id"] = repeated["runs"][0]["source"]["run_id"]
    with pytest.raises(ContractError, match="duplicate source run"):
        validate_document("studio_report", repeated)


@pytest.mark.parametrize(
    "field_path",
    [
        ("source",),
        ("facets",),
        ("profile", "engine"),
        ("profile", "engine", "backend"),
        ("profile", "machine", "logical_cpus"),
        ("summary", "stop_reason"),
        ("dashboard", "latency"),
        ("dashboard", "latency", "p50"),
        ("dashboard", "minimum_available_reason"),
    ],
)
def test_every_nested_required_field_is_present(tmp_path, field_path):
    data, source = loaded(tmp_path / "run")
    report = build(tmp_path, data, source)
    broken = deepcopy(report)
    target = broken["runs"][0]
    for key in field_path[:-1]:
        target = target[key]
    del target[field_path[-1]]
    with pytest.raises(ContractError, match="required field missing"):
        validate_document("studio_report", broken)


def test_request_and_engine_fields_are_closed(tmp_path):
    data, source = loaded(tmp_path / "run")
    report = build(tmp_path, data, source)

    broken = deepcopy(report)
    del broken["runs"][0]["requests"][0]["category_label"]
    with pytest.raises(ContractError, match="required field missing"):
        validate_document("studio_report", broken)

    unknown = deepcopy(report)
    unknown["runs"][0]["extra"] = 1
    with pytest.raises(ContractError, match="unknown field"):
        validate_document("studio_report", unknown)
    unknown = deepcopy(report)
    unknown["runs"][0]["profile"]["engine"]["release"] = "x"
    with pytest.raises(ContractError, match="unknown field"):
        validate_document("studio_report", unknown)


def test_explicit_null_is_allowed_only_where_nullable(tmp_path):
    data, source = loaded(tmp_path / "run")
    report = build(tmp_path, data, source)
    report["runs"][0]["threads"] = None
    report["runs"][0]["profile"]["title"] = None
    report["runs"][0]["profile"]["engine"]["binary_path"] = None
    report["runs"][0]["profile"]["machine"].update(cpu=None, logical_cpus=None)
    report["runs"][0]["requests"][0]["quality"] = None
    validate_document("studio_report", report)

    broken = deepcopy(report)
    broken["runs"][0]["model"] = None
    with pytest.raises(ContractError):
        validate_document("studio_report", broken)
    broken = deepcopy(report)
    broken["runs"][0]["threads"] = True
    with pytest.raises(ContractError):
        validate_document("studio_report", broken)
    broken = deepcopy(report)
    broken["runs"][0]["dashboard"]["quality"]["numerator"] = True
    with pytest.raises(ContractError, match="invalid type"):
        validate_document("studio_report", broken)


def test_empty_requests_keep_latency_and_quality_explicitly_empty(tmp_path):
    data, source = loaded(tmp_path / "run")
    data["requests"] = []
    data["summary"]["quality"]["Q01"] = {}
    data["summary"]["completion_rate"].update(numerator=0, denominator=0, excluded=0, value=None)
    report = build(tmp_path, data, source)
    dashboard = report["runs"][0]["dashboard"]
    assert report["runs"][0]["requests"] == []
    assert dashboard["categories"] == []
    assert dashboard["quality"] == {
        "numerator": 0,
        "denominator": 0,
        "excluded": 0,
        "complete": False,
        "reason": "quality_incomplete_or_not_applicable",
    }
    assert dashboard["latency"] == {
        "sample_count": 0,
        "excluded": 0,
        "min": None,
        "p50": None,
        "p95": None,
        "max": None,
    }


def test_request_projection_uses_score_quality_and_valid_timestamps(tmp_path):
    data, source = loaded(tmp_path / "run")
    data["requests"][0]["category"] = "custom"
    data["requests"][0]["score"] = {"quality_state": "not_applicable"}
    data["requests"][1].pop("score")
    data["requests"][2]["t_terminal_ns"] = data["requests"][2]["t_send_ns"] - 1
    run = build(tmp_path, data, source)["runs"][0]
    assert [row["category_label"] for row in run["requests"]] == ["custom", "指令遵循", "指令遵循"]
    assert [row["quality"] for row in run["requests"]] == ["not_applicable", None, None]
    assert run["requests"][2]["latency_ms"] is None
    assert run["dashboard"]["latency"]["sample_count"] == 2
    assert run["dashboard"]["latency"]["excluded"] == 1


def test_dashboard_categories_stay_in_the_six_known_classes(tmp_path):
    data, source = loaded(tmp_path / "run")
    data["summary"]["quality"]["Q01"] = {
        name: {
            "valid": 1,
            "passed": 1,
            "unscorable": 0,
            "excluded": 0,
            "rate": {"numerator": 1, "denominator": 1, "excluded": 0, "value": 1.0, "reason": None},
        }
        for name in CATEGORIES
    }
    run = build(tmp_path, data, source)["runs"][0]
    assert [row["id"] for row in run["dashboard"]["categories"]] == list(CATEGORIES)
    assert run["dashboard"]["quality"] == {
        "numerator": 6,
        "denominator": 6,
        "excluded": 0,
        "complete": True,
        "reason": None,
    }

    data["summary"]["quality"]["Q01"]["qa"]["rate"].update(value=None, reason="missing")
    run = build(tmp_path, data, source)["runs"][0]
    assert run["dashboard"]["quality"]["complete"] is False
    assert run["dashboard"]["quality"]["reason"] == "quality_incomplete_or_not_applicable"


def test_writer_publishes_only_studio_json_and_reader_never_verifies_sources(tmp_path):
    root = fixture_run(tmp_path / "run")
    out = tmp_path / "studio"
    written = write_studio_report([root], out)
    assert [path.name for path in out.iterdir()] == ["studio.json"]
    assert read_studio_report(out) == written
    raw = (out / "studio.json").read_text(encoding="utf-8")
    assert "verified" not in raw and "<html" not in raw

    shutil.rmtree(root)
    assert read_studio_report(out) == written


def test_reader_rejects_duplicate_keys_non_finite_numbers_and_tampered_structure(tmp_path):
    root = fixture_run(tmp_path / "run")
    out = tmp_path / "studio"
    written = write_studio_report([root], out)
    path = out / "studio.json"

    duplicate = '{"schema_version":3,"kind":"studio-report.v1","kind":"studio-report.v1","runs":[]}'
    path.write_text(duplicate, encoding="utf-8")
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        read_studio_report(out)

    text = json.dumps(written)
    count = written["runs"][0]["dashboard"]["latency"]["sample_count"]
    path.write_text(text.replace(f'"sample_count": {count}', '"sample_count": NaN', 1), "utf-8")
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        read_studio_report(out)

    broken = deepcopy(written)
    broken["runs"][0]["threads"] = "six"
    path.write_text(json.dumps(broken), encoding="utf-8")
    with pytest.raises(ContractError):
        read_studio_report(out)


def test_output_never_overlaps_sources_or_reuses_existing_directories(tmp_path):
    root = fixture_run(tmp_path / "run")
    with pytest.raises(EvidenceError, match="report_output_inside_original_evidence"):
        write_studio_report([root], root / "studio")
    existing = tmp_path / "existing"
    existing.mkdir()
    with pytest.raises(FileExistsError):
        write_studio_report([root], existing)
    assert (root / "manifest.json").exists()
    assert list(existing.iterdir()) == []


def test_exported_schema_matches_document_and_rejects_forbidden_shapes(tmp_path):
    data, source = loaded(tmp_path / "run")
    report = build(tmp_path, data, source)
    schema = export_schema("studio_report")
    assert schema["$id"].endswith(":studio_report")
    Draft202012Validator.check_schema(schema)
    Draft202012Validator(schema).validate(report)

    broken = deepcopy(report)
    broken["runs"][0]["extra"] = 1
    assert not Draft202012Validator(schema).is_valid(broken)
    broken = deepcopy(report)
    broken["runs"][0]["threads"] = True
    assert not Draft202012Validator(schema).is_valid(broken)
    broken = deepcopy(report)
    broken["runs"][0]["dashboard"]["sampled_peak_rss_bytes"] = -1
    assert not Draft202012Validator(schema).is_valid(broken)
