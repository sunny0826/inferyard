"""Cost harness must reject successful-but-incomplete output and real worker failures."""

import importlib.util
import json
import sys
from pathlib import Path

import pytest

SCRIPTS = Path(__file__).resolve().parents[2] / "scripts"
sys.path.insert(0, str(SCRIPTS))
spec = importlib.util.spec_from_file_location("cost_harness", SCRIPTS / "benchmark_cost.py")
harness = importlib.util.module_from_spec(spec)
spec.loader.exec_module(harness)
from benchmark_cost_compare import check_expected, compare_outputs, require_equal  # noqa: E402

TREE = SCRIPTS.parent


def save(path, value):
    path.write_text(json.dumps(value))


@pytest.fixture(scope="module")
def corpus(tmp_path_factory):
    out = tmp_path_factory.mktemp("cost-benchmark") / "corpus"
    harness.run_child(TREE, "prepare", out)
    return out


@pytest.fixture
def reports(tmp_path, corpus):
    left, right = tmp_path / "left", tmp_path / "right"
    harness.run_child(TREE, "report", left, corpus)
    harness.run_child(TREE, "report", right, corpus)
    return left, right


def test_real_reports_match_and_refuse_missing_last_row(reports):
    left, right = reports
    compare_outputs(left, right, "report")
    path = right / "result.json"
    value = json.loads(path.read_text())
    value["runs"][-1]["requests"].pop()
    save(path, value)
    with pytest.raises(ValueError, match="content_difference:report_index"):
        compare_outputs(left, right, "report")


def test_successful_report_cannot_hide_lost_svg_or_html(reports):
    left, right = reports
    html = right / "report/report.html"
    content = html.read_text()
    assert "鹈" in content
    html.write_text(content.replace("鹈", "", 1))
    with pytest.raises(ValueError, match="complete_html"):
        compare_outputs(left, right, "report")


def test_successful_report_cannot_hide_disk_index_change(reports):
    left, right = reports
    path = right / "report/index.json"
    value = json.loads(path.read_text())
    value["runs"].pop()
    save(path, value)
    with pytest.raises(ValueError, match="candidate_disk_index"):
        compare_outputs(left, right, "report")


def test_hash_and_numeric_semantics_are_not_discarded(tmp_path):
    left, right = tmp_path / "a", tmp_path / "b"
    left.mkdir()
    right.mkdir()
    save(left / "result.json", {"answer_sha256": "a" * 64, "count": 1})
    save(right / "result.json", {"answer_sha256": "b" * 64, "count": 1})
    with pytest.raises(ValueError, match="content_difference"):
        compare_outputs(left, right, "read_trial")
    with pytest.raises(ValueError, match="content_difference"):
        require_equal({"count": 1}, {"count": True}, "count")


def test_real_nonzero_worker_is_rejected_and_logs_preserved(tmp_path, corpus):
    out = tmp_path / "failed"
    with pytest.raises(ValueError, match="unexpected_exit"):
        harness.run_child(TREE, "not-an-operation", out, corpus)
    record = json.loads((out / "process.json").read_text())
    assert record["exit_code"] != 0
    assert "ValueError" in (out / "stderr.txt").read_text()
    with pytest.raises(FileExistsError):
        harness.run_child(TREE, "read_trial", out, corpus)


def test_actual_corrupted_evidence_matches_rejection_matrix(tmp_path, corpus):
    harness.rejection_matrix(TREE, corpus, tmp_path / "matrix")
    rows = json.loads((tmp_path / "matrix/rejection-matrix.json").read_text())
    assert [r["observed"]["exit_code"] for r in rows] == [4, 4, 4, 2]


def test_independent_fixture_expectation_rejects_matching_but_incomplete_results(reports, corpus):
    for root in reports:
        check_expected(root, corpus, "report")
        path = root / "result.json"
        value = json.loads(path.read_text())
        value["runs"][-1]["requests"].pop()
        save(path, value)
        with pytest.raises(ValueError, match="fixture_answer_completeness"):
            check_expected(root, corpus, "report")


def test_report_seal_must_match_actual_artifact_bytes(reports):
    left, right = reports
    path = right / "report/artifact-manifest.json"
    value = json.loads(path.read_text())
    value["files"]["report.html"] = "a" * 64
    save(path, value)
    with pytest.raises(ValueError, match="report_seal"):
        compare_outputs(left, right, "report")


def test_multirun_report_and_separate_comparison_are_both_exercised(reports, corpus, tmp_path):
    declaration = json.loads((corpus / "corpus.json").read_text())
    result = json.loads((reports[0] / "result.json").read_text())
    assert len(result["runs"]) >= 3
    assert len({r["source"]["run_id"] for r in result["runs"]}) == declaration["run_count"]
    assert result["comparison"] is None
    paired = json.loads((corpus / "reference-report/index.json").read_text())
    assert len(paired["runs"]) == len(declaration["comparison_run_indices"]) == 2
    assert paired["comparison"] is not None
    for operation in ("verify", "rerender", "verify_multirun", "rerender_multirun"):
        out = tmp_path / operation
        harness.run_child(TREE, operation, out, corpus)
        response = json.loads((out / "stdout.txt").read_text())
        assert response["details"]["render_checked"] == operation.startswith("rerender")


def test_empty_secret_clean_preserves_all_nested_input(tmp_path, corpus):
    out = tmp_path / "empty"
    harness.run_child(TREE, "redact_empty", out, corpus)
    value = json.loads((out / "result.json").read_text())
    assert value["credential_probe"] == "synthetic-secret"
    original = tmp_path / "read"
    harness.run_child(TREE, "read_trial", original, corpus)
    require_equal(value["trials"], json.loads((original / "result.json").read_text()), "clean")


def test_svg_count_uses_independent_declaration_and_rejects_missing_gallery(reports, corpus):
    root = reports[0]
    check_expected(root, corpus, "report")
    path = root / "result.json"
    value = json.loads(path.read_text())
    svg_run = next(r for r in value["runs"] if r["svg_gallery"])
    svg_run["svg_gallery"].pop()
    save(path, value)
    with pytest.raises(ValueError, match="svg_gallery_count"):
        check_expected(root, corpus, "report")
