"""The canonical CLI verifies real offline artifacts and preserves source evidence."""

import json
import socket
from dataclasses import replace
from pathlib import Path

import pytest

import inferyard.cli as cli
from inferyard.config.loader import LoadedConfig, load_config
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
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

    # Bind the synthetic model before sealing; public policy requires this
    # declaration without reading a real model or starting its service.
    monkeypatch.setattr("tests.helpers.load_config", portable_config)
    return fixture_run


def test_canonical_artifact_workflow_is_offline_and_checks_comparison_html(
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
        ("report", ["report", "--runs", left, "--out", tmp_path / "report"], "report-check"),
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
        derived_before = files(root)
        verified = invoke(capsys, ["verify", "--path", root])
        assert verified["command"] == "verify"
        assert verified["details"]["artifact_type"] == kind
        assert invoke(capsys, [legacy, "--run", root])["command"] == legacy
        assert files(root) == derived_before
    public = invoke(capsys, ["verify", "--path", tmp_path / "public", "--source-run", left])
    assert public["details"]["source_projection_verified"]
    html = tmp_path / "comparison/report.html"
    html.write_text(html.read_text() + "<p>tampered</p>")
    failed = invoke(capsys, ["verify", "--path", html.parent], 4)
    assert failed["limitations"] == ["presentation_bytes_changed"]
    assert before == {"left": files(left), "right": files(right)}


@pytest.mark.parametrize("mapping", ["missing-equals", "=new", "old="])
def test_invalid_source_mapping_is_an_input_error(tmp_path, capsys, mapping):
    from inferyard.reporting.report import write_report

    source = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    write_report([source], out)
    invoke(capsys, ["verify", "--path", out, "--source-root", mapping], 2)


def test_public_configuration_verification_keeps_match_and_mismatch_semantics(
    tmp_path, capsys, monkeypatch, portable_fixture
):
    source = portable_fixture(tmp_path / "source", states=["completed"])
    public = tmp_path / "public"
    invoke(capsys, ["public", "package", "--run", source, "--out", public])
    data = read_trial(source)
    loaded = LoadedConfig(
        Path("synthetic.toml"),
        Document.parse("config", data["config"]),
        Document.parse("bundle", data["bundle"]),
        "a" * 64,
        "b" * 64,
        (),
    )
    monkeypatch.setattr(cli, "load_config", lambda path: loaded)
    matched = invoke(capsys, ["verify", "--path", public, "--config", "synthetic.toml"])
    assert matched["status"] == "matched"
    assert matched["completeness"] == "incomplete"
    assert matched["details"]["declared_configuration_matches"]
    assert not matched["details"]["ready_to_run"]
    legacy = invoke(capsys, ["public-config-check", "--run", public, "--config", "synthetic.toml"])
    assert legacy["details"] == {
        key: value for key, value in matched["details"].items() if key != "artifact_type"
    }
    changed = loaded.config.to_dict()
    changed["conditions"]["threads"] += 1
    loaded = replace(loaded, config=Document.parse("config", changed))
    mismatch = invoke(
        capsys,
        ["verify", "--path", public, "--source-run", source, "--config", "synthetic.toml"],
        3,
    )
    assert mismatch["status"] == "mismatch"
    assert not mismatch["details"]["ready_to_run"]
    assert "conditions.threads" in mismatch["details"]["mismatched_fields"]
    assert mismatch["details"]["source_projection_verified"]
