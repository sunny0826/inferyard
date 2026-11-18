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
