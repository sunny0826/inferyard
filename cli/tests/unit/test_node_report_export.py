from inferyard.contracts.validation import validate_document
from inferyard.node.report_export import export_report, list_reports
from tests.helpers import fixture_run


def test_raw_and_existing_report_exports_have_distinct_provenance(tmp_path):
    run = fixture_run(tmp_path / "run")
    raw = export_report(state_dir=tmp_path / "state", run_path=str(run), out=str(tmp_path / "raw"))
    assert raw["verified_from_raw"] is True
    assert raw["verification"] == "raw_evidence"
    assert raw["report"]["schema_version"] == 3
    validate_document("studio_report", raw["report"])

    existing = export_report(
        state_dir=tmp_path / "state",
        report_path=raw["path"],
        out=str(tmp_path / "existing"),
    )
    assert existing["verified_from_raw"] is False
    assert existing["verification"] == "schema_only"
    assert existing["report"] == raw["report"]
    assert list_reports(tmp_path / "state" / "reports") == []
