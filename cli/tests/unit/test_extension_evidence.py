import json
import shutil

import pytest

from inferyard.cli import main
from inferyard.errors import EvidenceError
from inferyard.extensions.extension_evidence import (
    ExtensionJournal,
    reduce_packet,
    save_packet,
    verify_packet,
)
from inferyard.provenance import tool_source_hash
from tests.unit.test_closed_concurrency import rows, spec


def test_sealed_extension_replay_rejects_raw_and_derived_tampering(tmp_path):
    packet = {"spec": spec(), "rows": rows(), "evidence_kind": "fixture"}
    path, result = save_packet(tmp_path, packet)
    before = {p.name: p.read_bytes() for p in path.iterdir()}
    assert verify_packet(path) == result
    assert before == {p.name: p.read_bytes() for p in path.iterdir()}
    (path / "summary.json").write_text("{}")
    with pytest.raises(EvidenceError):
        verify_packet(path)


def test_cli_fixture_reports_never_inherit_live_qualification(tmp_path, capsys):
    source = tmp_path / "input.json"
    source.write_text(json.dumps({"spec": spec(), "rows": rows(), "evidence_kind": "fixture"}))
    assert (
        main(["extension", "replay", "--packet", str(source), "--out", str(tmp_path / "out")]) == 0
    )
    data = json.loads(capsys.readouterr().out)
    assert not data["details"]["hardware_qualified"]
    assert main(["verify", "--path", data["evidence_dir"]]) == 0
    checked = json.loads(capsys.readouterr().out)
    assert checked["details"] == {**data["details"], "artifact_type": "extension"}
    source.write_text(json.dumps({"spec": spec(), "rows": rows(), "evidence_kind": "live"}))
    assert (
        main(["extension", "replay", "--packet", str(source), "--out", str(tmp_path / "forged")])
        != 0
    )
    capsys.readouterr()
    assert not (tmp_path / "forged").exists()


def test_offline_freeze_binds_config_corpus_and_installed_source(tmp_path, config_path, capsys):
    from inferyard.config.loader import load_config
    from inferyard.extensions.workflow import read_plan
    from inferyard.provenance import tool_source_hash

    loaded = load_config(config_path)
    plan = spec()
    plan["case_ids"] = [loaded.bundle.to_dict()["cases"][0]["case_id"]]
    path = tmp_path / "draft.json"
    path.write_text(json.dumps(plan))
    out = tmp_path / "frozen"
    assert (
        main(
            [
                "extension",
                "freeze",
                "--spec",
                str(path),
                "--config",
                str(config_path),
                "--out",
                str(out),
            ]
        )
        == 0
    )
    data = json.loads(capsys.readouterr().out)
    assert data["details"]["tool_source_sha256"] == tool_source_hash()
    assert read_plan(out / "plan.json", loaded) == data["details"]
    raw = json.loads((out / "plan.json").read_text())
    raw["spec"]["concurrency"] = 1
    (out / "plan.json").write_text(json.dumps(raw))
    with pytest.raises(EvidenceError, match="hash_mismatch"):
        read_plan(out / "plan.json", loaded)


def _seal_packet(root, run, packet, *, extra=None, presentation=False):
    journal = ExtensionJournal(root)
    try:
        journal.snapshot("run.json", run)
        journal.snapshot("packet.json", packet)
        journal.snapshot("summary.json", reduce_packet(packet))
        for name, value in (extra or {}).items():
            journal.snapshot(name, value)
        if presentation:
            journal.text_snapshot("report.html", "<!doctype html><title>retired</title>")
        journal.seal()
    finally:
        journal.close()
    return journal.path


def _v1_packet(tmp_path):
    packet = {"spec": spec(), "rows": rows(), "evidence_kind": "fixture"}
    return _seal_packet(
        tmp_path / "v1",
        {
            "kind": "extension_packet.v1",
            "origin": "offline_fixture",
            "tool_source_sha256": tool_source_hash(),
            "evidence_kind": "fixture",
        },
        packet,
        presentation=True,
    )


def test_saved_packet_is_v2_and_has_no_presentation_snapshot(tmp_path):
    packet = {"spec": spec(), "rows": rows(), "evidence_kind": "fixture"}
    path, _ = save_packet(tmp_path, packet)
    assert json.loads((path / "run.json").read_text())["kind"] == "extension_packet.v2"
    assert not (path / "report.html").exists()
    manifest = json.loads((path / "manifest.json").read_text())
    assert "report.html" not in manifest["files"]


def test_complete_v1_packet_is_unsupported_but_damaged_v1_is_integrity_error(tmp_path, capsys):
    path = _v1_packet(tmp_path)
    assert main(["verify", "--path", str(path)]) == 2
    result = json.loads(capsys.readouterr().out)
    assert result["limitations"] == ["unsupported_format"]
    assert result["details"] == {
        "artifact": "extension packet",
        "saved_version": "extension_packet.v1",
        "supported_versions": ["extension_packet.v2"],
    }
    (path / "summary.json").write_text("{}")
    assert main(["verify", "--path", str(path)]) == 4
    assert json.loads(capsys.readouterr().out)["limitations"] == ["evidence_error"]


def _seal_observer(parent):
    from inferyard.evidence.storage import EvidenceStore, sha256_file

    store = EvidenceStore(parent / "observer-arms" / "arm-0")
    try:
        store.snapshot("marker.json", {"kind": "observer_arm"})
        store.seal()
    finally:
        store.close()
    return {
        "path": store.path.relative_to(parent).as_posix(),
        "manifest_sha256": sha256_file(store.path / "manifest.json"),
    }


def _live_v1_packet(tmp_path, *, run_kind="extension_packet.v1", run_evidence="live", summary=None):
    packet = {"spec": spec(), "rows": rows(), "evidence_kind": "live"}
    journal = ExtensionJournal(tmp_path / "live-v1")
    try:
        child = _seal_observer(journal.path)
        journal.snapshot(
            "run.json",
            {
                "kind": run_kind,
                "origin": "live_driver",
                "tool_source_sha256": "a" * 64,
                "evidence_kind": run_evidence,
            },
        )
        journal.snapshot("packet.json", packet)
        journal.snapshot("summary.json", reduce_packet(packet) if summary is None else summary)
        journal.snapshot("plan.json", {"spec": spec(), "tool_source_sha256": "a" * 64})
        journal.snapshot("identity.json", {"verification": "verified"})
        journal.snapshot("child-evidence.json", [child])
        journal.text_snapshot("report.html", "<!doctype html><title>retired</title>")
        journal.seal()
    finally:
        journal.close()
    return journal.path, child


def test_complete_live_v1_packet_is_unsupported_after_original_evidence_checks(tmp_path, capsys):
    path, _ = _live_v1_packet(tmp_path)
    assert main(["verify", "--path", str(path)]) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["unsupported_format"]


def test_v1_live_packet_with_missing_or_changed_child_evidence_is_integrity_error(
    tmp_path, capsys
):
    missing, child = _live_v1_packet(tmp_path / "missing")
    shutil.rmtree(missing / child["path"])
    assert main(["verify", "--path", str(missing)]) == 4
    assert json.loads(capsys.readouterr().out)["status"] == "error"
    changed, child = _live_v1_packet(tmp_path / "changed")
    (changed / child["path"] / "marker.json").write_text("changed")
    assert main(["verify", "--path", str(changed)]) == 4
    assert json.loads(capsys.readouterr().out)["status"] == "error"


def test_v1_packet_with_mismatched_evidence_or_replay_is_integrity_error(tmp_path, capsys):
    mismatched, _ = _live_v1_packet(tmp_path / "kind", run_evidence="fixture")
    assert main(["verify", "--path", str(mismatched)]) == 4
    assert json.loads(capsys.readouterr().out)["limitations"] == ["evidence_error"]
    replay, _ = _live_v1_packet(tmp_path / "replay", summary={"bogus": True})
    assert main(["verify", "--path", str(replay)]) == 4
    assert json.loads(capsys.readouterr().out)["limitations"] == ["evidence_error"]


def test_v2_live_packet_still_binds_plan_identity_and_child_evidence(tmp_path):
    packet = {"spec": spec(), "rows": rows(), "evidence_kind": "live"}
    plan = {"spec": spec(), "tool_source_sha256": "a" * 64}
    path = _seal_packet(
        tmp_path / "live",
        {
            "kind": "extension_packet.v2",
            "origin": "live_driver",
            "tool_source_sha256": "a" * 64,
            "evidence_kind": "live",
        },
        packet,
        extra={
            "plan.json": plan,
            "identity.json": {"verification": "verified"},
            "child-evidence.json": [],
        },
    )
    assert verify_packet(path) == reduce_packet(packet)


def test_v2_live_packet_rejects_unverified_identity(tmp_path):
    packet = {"spec": spec(), "rows": rows(), "evidence_kind": "live"}
    path = _seal_packet(
        tmp_path / "live",
        {
            "kind": "extension_packet.v2",
            "origin": "live_driver",
            "tool_source_sha256": "a" * 64,
            "evidence_kind": "live",
        },
        packet,
        extra={
            "plan.json": {"spec": spec(), "tool_source_sha256": "a" * 64},
            "identity.json": {"verification": "unverified"},
            "child-evidence.json": [],
        },
    )
    with pytest.raises(EvidenceError, match="extension_live_identity_unverified"):
        verify_packet(path)


def test_live_evidence_cannot_be_saved_as_an_offline_origin(tmp_path):
    packet = {"spec": spec(), "rows": rows(), "evidence_kind": "live"}
    path, _ = save_packet(tmp_path, packet)
    with pytest.raises(EvidenceError, match="extension_live_origin_unverified"):
        verify_packet(path)
