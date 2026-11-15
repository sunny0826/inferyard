"""Current Studio publication rejects duplicate sources and never links comparisons."""

import json

import pytest

from inferyard.errors import EvidenceError
from inferyard.reporting.studio_report import build_studio_report, write_studio_report
from tests.helpers import fixture_run


def test_multi_run_studio_report_rejects_duplicate_sources(tmp_path):
    roots = [fixture_run(tmp_path / str(i)) for i in range(3)]
    out = tmp_path / "report"
    document = write_studio_report(roots, out)
    assert len(document["runs"]) == 3
    assert {path.name for path in out.iterdir()} == {"studio.json"}
    with pytest.raises(EvidenceError, match="duplicate_report_run"):
        build_studio_report([roots[0], roots[1], roots[0]])


def test_cost_harness_measures_only_the_remaining_json_work():
    from scripts import benchmark_cost

    assert set(benchmark_cost.OPERATIONS) == {
        "read_trial",
        "studio_json",
        "resources",
        "redact",
        "redact_empty",
        "json",
        "redact_stream",
        "redact_stream_empty",
    }
    assert not any(
        token in operation
        for operation in benchmark_cost.OPERATIONS
        for token in ("report", "verify", "rerender", "html")
    )


def test_cost_compare_requires_exact_studio_json(tmp_path):
    from scripts import benchmark_cost_compare

    document = {"schema_version": 3, "kind": "studio-report.v1", "runs": []}

    def write(root, value):
        (root / "report").mkdir(parents=True)
        raw = json.dumps(value)
        (root / "result.json").write_text(raw, encoding="utf-8")
        (root / "report/studio.json").write_text(raw, encoding="utf-8")

    left, right = tmp_path / "left", tmp_path / "right"
    write(left, document)
    write(right, document)
    assert benchmark_cost_compare.compare_outputs(left, right, "studio_json") == []

    changed = {**document, "schema_version": 4}
    (right / "result.json").write_text(json.dumps(changed), encoding="utf-8")
    with pytest.raises(ValueError, match="content_difference:studio_json"):
        benchmark_cost_compare.compare_outputs(left, right, "studio_json")


def test_cost_answers_are_not_part_of_the_studio_projection(tmp_path):
    from scripts import benchmark_cost_compare

    corpus = tmp_path / "corpus"
    corpus.mkdir()
    (corpus / "corpus.json").write_text(
        json.dumps({"run_count": 1, "cases": 1, "samples": 2, "answer_sha256": []}),
        encoding="utf-8",
    )
    out = tmp_path / "out"
    out.mkdir()
    result = {
        "schema_version": 3,
        "kind": "studio-report.v1",
        "runs": [
            {
                "source": {"run_id": "run-1", "path": "/synthetic/run-1"},
                "requests": [{"case_id": "case-1", "content": "raw answer"}],
            }
        ],
    }
    (out / "result.json").write_text(json.dumps(result), encoding="utf-8")
    with pytest.raises(ValueError, match="studio_json_scope"):
        benchmark_cost_compare.check_expected(out, corpus, "studio_json")
