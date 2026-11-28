from inferyard.contracts.validation import validate_document
from inferyard.node.report_export import export_report, list_reports
from tests.helpers import fixture_run


def test_raw_and_existing_report_exports_have_distinct_provenance(tmp_path):
    run = fixture_run(tmp_path / "run")
    raw = export_report(state_dir=tmp_path / "state", run_path=str(run), out=str(tmp_path / "raw"))
    assert raw["verified_from_raw"] is True
    assert raw["verification"] == "raw_evidence"
    assert raw["report"]["schema_version"] == 3
    assert raw["rerunnable_run_paths"] == [str(run.resolve())]
    assert raw["resumable_run_paths"] == []
    validate_document("studio_report", raw["report"])

    existing = export_report(
        state_dir=tmp_path / "state",
        report_path=raw["path"],
        out=str(tmp_path / "existing"),
    )
    assert existing["verified_from_raw"] is False
    assert existing["verification"] == "schema_only"
    assert existing["report"] == raw["report"]
    assert existing["rerunnable_run_paths"] == [str(run.resolve())]
    assert existing["resumable_run_paths"] == []
    assert list_reports(tmp_path / "state" / "reports") == []

    synced = export_report(state_dir=tmp_path / "state", report_path=raw["path"])
    assert synced["path"] == raw["path"]
    assert synced["report"] == raw["report"]
    assert synced["rerunnable_run_paths"] == [str(run.resolve())]
    assert synced["resumable_run_paths"] == []
    assert not (tmp_path / "state" / "reports").exists()
