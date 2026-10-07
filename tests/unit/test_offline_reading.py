"""Direct raw reading, sealed bytes and explicit source relocation boundaries."""

import hashlib
import shutil
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest, VerificationOptions
from inferyard.application.verification import execute
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.reporting.report import verify_report, write_report
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
    from inferyard.evidence.ledger import read_trial

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


def test_comparison_and_linked_report_move_and_keep_source_binding(tmp_path):
    from inferyard.reporting.comparison_report import execute as compare

    base = tmp_path / "original"
    roots = [fixture_run(base / side) for side in ("left", "right")]
    compare(CommandRequest("compare", left=roots[0], right=roots[1], out=base / "comparison"))
    write_report(roots, base / "report", comparison_path=base / "comparison")
    moved = tmp_path / "moved"
    base.rename(moved)
    assert execute(CommandRequest("verify", run=moved / "comparison"))[0] == 0
    assert verify_report(moved / "report")["sources_verified"]
    source = moved / roots[0].relative_to(base)
    (source / "events.jsonl").write_bytes(b"{}\n")
    with pytest.raises(EvidenceError):
        verify_report(moved / "report")


def test_corrupt_seal_is_not_downgraded_to_partial(tmp_path):
    root = fixture_run(tmp_path)
    (root / "events.jsonl").write_bytes(b"{}\n")
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=root))


def test_capability_definition_is_interpreted_once_from_bound_source(tmp_path, monkeypatch):
    from inferyard.evidence import engine_capabilities
    from inferyard.evidence.ledger import read_trial

    root = fixture_run(tmp_path)
    calls = []
    original = engine_capabilities.bound_capabilities

    def counted(*args, **kwargs):
        calls.append(1)
        return original(*args, **kwargs)

    monkeypatch.setattr(engine_capabilities, "bound_capabilities", counted)
    read_trial(root)
    assert len(calls) == 1
    (root / "service.props.json").write_bytes(json_bytes({"engine_build_verified": True}))
    result = original(root, {"service.props.json": {}})
    assert not result.verified


def test_report_moves_with_sources_and_does_not_render_during_verify(tmp_path, monkeypatch):
    import inferyard.reporting.report_common as common

    original = tmp_path / "original"
    original.mkdir()
    root = fixture_run(original)
    out = original / "report"
    saved = write_report([root], out)
    assert saved["report_format_version"] == 7
    assert not Path(saved["runs"][0]["source"]["path"]).is_absolute()
    before = (out / "report.html").read_bytes()
    moved = tmp_path / "moved"
    original.rename(moved)

    def no_render(*args):
        raise AssertionError("renderer must not be needed")

    monkeypatch.setattr(common, "render_report_html", no_render)
    checked = verify_report(moved / "report")
    assert checked["bytes_verified"] and checked["semantic_verified"]
    assert not checked["render_checked"]
    assert checked.get("html_matches_index") is not True
    assert (moved / "report/report.html").read_bytes() == before


def test_explicit_source_root_mapping_preserves_saved_bytes(tmp_path):
    root = fixture_run(tmp_path / "original")
    out = tmp_path / "report"
    write_report([root], out)
    before = (out / "index.json").read_bytes()
    destination = tmp_path / "elsewhere"
    shutil.move(root, destination)
    options = VerificationOptions(((root.resolve(), destination.resolve()),))
    assert verify_report(out, options=options)["sources_verified"]
    assert (out / "index.json").read_bytes() == before


@pytest.mark.parametrize("reseal", [False, True])
def test_changed_report_data_is_rejected_even_with_recomputed_byte_seal(tmp_path, reseal):
    root = fixture_run(tmp_path)
    out = tmp_path / "report"
    write_report([root], out)
    saved = read_json(out / "index.json")
    saved["runs"][0]["requests"][0]["content"] = "invented answer"
    raw = json_bytes(saved)
    (out / "index.json").write_bytes(raw)
    if reseal:
        seal = read_json(out / "artifact-manifest.json")
        seal["files"]["index.json"] = hashlib.sha256(raw).hexdigest()
        (out / "artifact-manifest.json").write_bytes(json_bytes(seal))
    with pytest.raises(EvidenceError, match="presentation_bytes_changed|recomputation_mismatch"):
        verify_report(out)


def test_changed_html_and_explicit_rerender(tmp_path, monkeypatch):
    import inferyard.reporting.report_common as common

    root = fixture_run(tmp_path)
    out = tmp_path / "report"
    write_report([root], out)
    assert verify_report(out, options=VerificationOptions(rerender=True))["render_checked"]
    monkeypatch.setattr(common, "render_report_html", lambda *args: "changed renderer")
    assert verify_report(out)["verified"]
    with pytest.raises(EvidenceError, match="html_recomputation"):
        verify_report(out, options=VerificationOptions(rerender=True))
    (out / "report.html").write_text("corrupt bytes")
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        verify_report(out)
