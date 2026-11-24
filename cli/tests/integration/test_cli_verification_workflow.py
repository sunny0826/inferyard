"""The canonical CLI verifies real offline artifacts and preserves source evidence."""

import json
import socket
from dataclasses import replace
from pathlib import Path

import pytest

import inferyard.cli as cli
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document
from inferyard.evidence.storage import sha256_file
from tests.helpers import fixture_run
from tests.unit.test_closed_concurrency import rows, spec


def files(root):
    return {str(p.relative_to(root)): sha256_file(p) for p in root.rglob("*") if p.is_file()}


def invoke(capsys, arguments, expected=0):
    assert cli.main([str(a) for a in arguments]) == expected
    captured = capsys.readouterr()
    if expected == 0:
        assert not captured.err
    assert len(captured.out.splitlines()) == 1
    return json.loads(captured.out)


@pytest.fixture
def portable_fixture(monkeypatch):
    def portable_config(path):
        loaded = load_config(path)
        config = loaded.config.to_dict()
        config["engine"]["startup_args"] += ["-m", config["model"]["local_path"]]
        return replace(loaded, config=Document.parse("config", config))

    monkeypatch.setattr("tests.helpers.load_config", portable_config)
    return fixture_run


def test_canonical_artifact_workflow_is_offline_and_rejects_retired_presentation(
    tmp_path, capsys, monkeypatch, portable_fixture
):
    def no_network(*args, **kwargs):
        raise AssertionError("offline command tried to open a socket")

    monkeypatch.setattr(socket, "socket", no_network)
    left = portable_fixture(tmp_path / "left", states=["completed"])
    right = portable_fixture(tmp_path / "right", states=["completed"], model="synthetic-B")
    before = {"left": files(left), "right": files(right)}
    packet = tmp_path / "fixture-packet.json"
    packet.write_text(json.dumps({"spec": spec(), "rows": rows(), "evidence_kind": "fixture"}))
    operations = [
        ("export", ["export", "--run", left, "--out", tmp_path / "export"], "export-check"),
        (
            "rescore",
            [
                "rescore",
                "--run",
                left,
                "--out",
                tmp_path / "rescore",
                "--scorer",
                "phase2.v1",
                "--reason",
                "synthetic offline regression",
            ],
            "rescore-check",
        ),
        (
            "public",
            ["public", "package", "--run", left, "--out", tmp_path / "public"],
            "public-check",
        ),
        (
            "comparison",
            ["compare", "--left", left, "--right", right, "--out", tmp_path / "comparison"],
            "compare-check",
        ),
        (
            "extension",
            ["extension", "replay", "--packet", packet, "--out", tmp_path / "extension"],
            "extension-check",
        ),
    ]
    for kind, arguments, legacy in operations:
        generated = invoke(capsys, arguments)
        if kind == "public":
            assert generated["command"] == "public package"
        if kind == "extension":
            assert generated["command"] == "extension replay"
            assert not generated["details"]["hardware_qualified"]
        root = Path(generated["evidence_dir"]) if kind == "extension" else tmp_path / kind
        if kind == "extension":
            assert json.loads((root / "run.json").read_text())["kind"] == "extension_packet.v2"
            assert not (root / "report.html").exists()
        derived_before = files(root)
        verified = invoke(capsys, ["verify", "--path", root])
        assert verified["command"] == "verify"
        assert verified["details"]["artifact_type"] == kind
        assert invoke(capsys, [legacy, "--run", root], 2)["limitations"] == ["invalid_input"]
        assert files(root) == derived_before
    public = invoke(capsys, ["verify", "--path", tmp_path / "public", "--source-run", left])
    assert public["details"]["source_projection_verified"]
    report = invoke(capsys, ["report", "--runs", left, "--out", tmp_path / "report"])
    assert report["status"] == "generated"
    studio = invoke(capsys, ["verify", "--path", tmp_path / "report"], 4)
    assert studio["limitations"] == ["studio_report_not_verifiable"]
    assert invoke(capsys, ["report-check", "--run", tmp_path / "report"], 2)["limitations"] == [
        "invalid_input"
    ]
    saved = json.loads((tmp_path / "comparison/comparison.json").read_text())
    saved["sides"][0]["run_id"] = "invented-run"
    (tmp_path / "comparison/comparison.json").write_text(json.dumps(saved))
    failed = invoke(capsys, ["verify", "--path", tmp_path / "comparison"], 4)
    assert failed["limitations"] == ["comparison_recomputation_mismatch"]
    assert before == {"left": files(left), "right": files(right)}
