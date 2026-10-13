import json

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import EvidenceError
from inferyard.extensions.extension_evidence import save_packet, verify_packet
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
