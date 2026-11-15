"""Direct raw reading, sealed bytes and explicit source relocation boundaries."""

import shutil

import pytest

from inferyard.application.types import CommandRequest
from inferyard.evidence.verification_options import VerificationOptions
from inferyard.application.verification import execute
from inferyard.errors import EvidenceError
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import read_json
from inferyard.reporting.comparison_report import verify_comparison, write_comparison
from inferyard.reporting.studio_report import read_studio_report, write_studio_report
from tests.helpers import fixture_run


def test_raw_run_and_unsealed_partial_are_read_without_report(tmp_path):
    root = fixture_run(tmp_path)
    assert execute(CommandRequest("verify", run=root))[0] == 0
    (root / "manifest.json").unlink()
    code, result = execute(CommandRequest("verify", run=root))
    assert code == 3 and result.status == "partial"
    assert result.details["integrity"] == "unsealed"
    assert result.details["verified"] is False
    assert result.details["sealed"] is False

    assert len(result.details["requests"]) == 3
    assert not (root / "report.html").exists()


@pytest.mark.parametrize("states", [["cancelled", "not_executed"], ["invalid", "not_executed"]])
def test_sealed_partial_run_verifies_without_promoting_execution(tmp_path, states):
    from inferyard.analysis.ledger import read_trial

    root = fixture_run(tmp_path, states=states)
    original = read_trial(root)
    code, result = execute(CommandRequest("verify", run=root))
    assert code == 0 and result.status == "verified"
    assert result.completeness == result.details["execution_completeness"] == "incomplete"
    assert result.details["sealed"] and result.details["verified"]
    assert result.details["stop_reason"] == original["summary"]["stop_reason"]
    assert result.details["requests"] == original["requests"]


def test_frozen_plan_and_empty_batch_are_read_without_derived_report(tmp_path, config_path):
    from inferyard.config.planning import write_plan
    from inferyard.runtime.batch_state import open_batch
    from tests.integration.test_phase2_plan import input_package

    source = input_package(tmp_path, config_path)
    frozen = tmp_path / "frozen"
    write_plan(source, frozen)
    for path in (frozen, frozen / "plan.json"):
        code, result = execute(CommandRequest("verify", run=path))
        assert code == 0 and result.status == "validated"
        assert result.details["integrity"] == "frozen_document"
    batch = tmp_path / "batch"
    open_batch(batch, frozen / "plan.json", diagnostic=True)
    code, result = execute(CommandRequest("verify", run=batch))
    assert code == 3 and result.status == "partial"
    assert result.details["trials"] == []
    assert result.details["verified"] is False

    original = read_json(batch / "batch.json")
    for key, value in (("tool_source_sha256", "invalid"), ("implementation_identity", {})):
        (batch / "batch.json").write_bytes(json_bytes({**original, key: value}))
        with pytest.raises(EvidenceError):
            execute(CommandRequest("verify", run=batch))


def test_comparison_moves_and_keeps_source_binding(tmp_path):
    base = tmp_path / "original"
    roots = [fixture_run(base / side) for side in ("left", "right")]
    write_comparison(
        CommandRequest("compare", left=roots[0], right=roots[1], out=base / "comparison")
    )
    moved = tmp_path / "moved"
    base.rename(moved)
    assert execute(CommandRequest("verify", run=moved / "comparison"))[0] == 0
    source = moved / roots[0].relative_to(base)
    (source / "events.jsonl").write_bytes(b"{}\n")
    with pytest.raises(EvidenceError):
        verify_comparison(moved / "comparison")


def test_corrupt_seal_is_not_downgraded_to_partial(tmp_path):
    root = fixture_run(tmp_path)
    (root / "events.jsonl").write_bytes(b"{}\n")
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=root))


def test_capability_definition_is_interpreted_once_from_bound_source(tmp_path):
    from inferyard.evidence import engine_capabilities

    root = fixture_run(tmp_path)
    (root / "service.props.json").write_bytes(json_bytes({"engine_build_verified": True}))
    result = engine_capabilities.bound_capabilities(root, {"service.props.json": {}})
    assert not result.verified


def test_studio_report_keeps_absolute_sources_and_reads_without_them(tmp_path):
    original = tmp_path / "original"
    original.mkdir()
    root = fixture_run(original)
    out = original / "report"
    saved = write_studio_report([root], out)
    assert saved["runs"][0]["source"]["path"] == str(root.resolve())
    before = (out / "studio.json").read_bytes()
    moved = tmp_path / "moved"
    original.rename(moved)
    assert read_studio_report(moved / "report")["kind"] == "studio-report.v1"
    assert (moved / "report/studio.json").read_bytes() == before
    with pytest.raises(EvidenceError, match="studio_report_not_verifiable"):
        execute(CommandRequest("verify", run=moved / "report"))


def test_explicit_source_root_mapping_preserves_saved_bytes(tmp_path):
    roots = [fixture_run(tmp_path / side) for side in ("left", "right")]
    out = tmp_path / "comparison"
    write_comparison(CommandRequest("compare", left=roots[0], right=roots[1], out=out))
    before = (out / "comparison.json").read_bytes()
    destination = tmp_path / "elsewhere"
    shutil.move(roots[0], destination)
    options = VerificationOptions(((roots[0].resolve(), destination.resolve()),))
    assert verify_comparison(out, source_roots=options.source_roots)["verified"]
    assert (out / "comparison.json").read_bytes() == before


def test_changed_comparison_data_is_rejected_by_recomputation(tmp_path):
    roots = [fixture_run(tmp_path / side) for side in ("left", "right")]
    out = tmp_path / "comparison"
    write_comparison(CommandRequest("compare", left=roots[0], right=roots[1], out=out))
    saved = read_json(out / "comparison.json")
    saved["sides"][0]["run_id"] = "invented-run"
    (out / "comparison.json").write_bytes(json_bytes(saved))
    with pytest.raises(EvidenceError, match="recomputation_mismatch|sources_or_order"):
        verify_comparison(out)
