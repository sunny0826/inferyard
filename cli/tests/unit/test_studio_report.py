"""Studio report is one projected JSON document; it never renders or verifies evidence."""

import json
import shutil
from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard.analysis.comparison_input import comparison_input
from inferyard.contracts.schemas import export_schema
from inferyard.errors import ContractError, EvidenceError
from inferyard.reporting.studio_report import (
    build_studio_report,
    read_studio_report,
    write_studio_report,
)
from tests.helpers import fixture_run


def loaded(root, **kwargs):
    return comparison_input(fixture_run(root, **kwargs))


def build(tmp_path, data, source):
    return build_studio_report([tmp_path / "run"], loaded=[(data, source)])


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
