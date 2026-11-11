"""Offline verification retains dedicated semantics and refuses weak fallbacks."""

import hashlib
import json
import os
import socket
import subprocess
from dataclasses import replace
from importlib import import_module
from pathlib import Path

import pytest

from inferyard import cli
from inferyard.application.types import CommandRequest, CommandResult
from inferyard.application.verification import execute
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError, EvidenceError
from inferyard.evidence.storage import json_bytes, read_json
from tests.helpers import fixture_run, symlink_or_skip


def marker(root, kind, filename):
    value = {}
    if kind == "overhead":
        value = {
            "protocol_sha256": "a" * 64,
            "status": "incomplete",
            "passed": False,
            "limitations": ["four_completed_runs_required"],
        }
    elif kind == "comparison":
        (root / "index.json").write_bytes(json_bytes({"report_format_version": 3}))
        (root / "report.html").write_text("synthetic report")
    (root / filename).write_bytes(json_bytes(value))


def digest_tree(root):
    return {
        str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in root.rglob("*")
        if path.is_file()
    }


def source_fixture(root, **kwargs):
    def portable_config(path):
        loaded = load_config(path)
        config = loaded.config.to_dict()
        config["engine"]["startup_args"] = ["-m", config["model"]["local_path"]]
        return replace(loaded, config=Document.parse("config", config))

    # Supply a valid declared model binding before the journal is constructed.
    with pytest.MonkeyPatch.context() as patched:
        patched.setattr("tests.helpers.load_config", portable_config)
        return fixture_run(root, **kwargs)


def make_artifact(tmp_path, kind):
    root = source_fixture(tmp_path / "sources")
    out = tmp_path / "artifact"
    if kind == "export":
        from inferyard.reporting.export import write_export

        write_export(out, run=root)
    elif kind == "rescore":
        from inferyard.reporting.rescore import write_rescore

        write_rescore(root, out, scorer_id="phase2.v2", reason="offline fixture regression")
    elif kind == "public":
        from inferyard.reporting.public_package import write_public

        write_public(root, out)
    elif kind == "report":
        from inferyard.reporting.report import write_report

        write_report([root], out)
    elif kind == "comparison":
        from inferyard.reporting.comparison_report import execute as compare

        right = source_fixture(tmp_path / "sources", model="synthetic-B")
        compare(CommandRequest("compare", left=root, right=right, out=out))
    elif kind == "extension":
        from inferyard.extensions.extension_evidence import save_packet
        from tests.unit.test_closed_concurrency import rows, spec

        out, _ = save_packet(out, {"spec": spec(), "rows": rows(), "evidence_kind": "fixture"})
    return root, out


@pytest.mark.parametrize(
    "kind", ["export", "rescore", "public", "report", "comparison", "extension"]
)
def test_real_offline_artifacts_replay_without_mutating_any_source(
    tmp_path, kind, monkeypatch, capsys
):
    root, out = make_artifact(tmp_path, kind)
    before = digest_tree(tmp_path)

    def forbid_live_io(*args, **kwargs):
        pytest.fail("offline verification attempted network or process IO")

    monkeypatch.setattr(socket.socket, "connect", forbid_live_io)
    monkeypatch.setattr(socket.socket, "connect_ex", forbid_live_io)
    monkeypatch.setattr(socket, "create_connection", forbid_live_io)
    monkeypatch.setattr(subprocess, "Popen", forbid_live_io)
    monkeypatch.setattr(os, "kill", forbid_live_io)
    argv = ["verify", "--path", str(out)]
    if kind == "public":
        argv += ["--source-run", str(root)]
    assert cli.main(argv) == 0
    result = json.loads(capsys.readouterr().out)
    assert result["command"] == "verify" and result["status"] == "verified"
    assert result["completeness"] == "complete"
    assert result["details"]["artifact_type"] == kind
    if kind == "comparison":
        assert result["details"]["comparison"]["details"]["verified"]
        assert result["details"]["report"]["details"]["semantic_verified"]
    elif kind == "extension":
        assert result["details"]["hardware_qualified"] is False
    elif kind == "public":
        assert result["details"]["source_projection_verified"] is True
    assert before == digest_tree(tmp_path)


@pytest.mark.parametrize(
    "kind,filename",
    [
        ("export", "metrics.csv"),
        ("rescore", "parent-analysis.json"),
        ("public", "REPRODUCE.md"),
        ("report", "report.html"),
        ("comparison", "report.html"),
        ("extension", "summary.json"),
    ],
)
def test_real_payload_corruption_is_rejected(tmp_path, kind, filename):
    _, out = make_artifact(tmp_path, kind)
    with (out / filename).open("ab") as stream:
        stream.write(b"tampered")
    before = digest_tree(tmp_path)
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=out))
    assert before == digest_tree(tmp_path)


@pytest.mark.parametrize(
    "filename",
    ["result.json", "repetition-summary.json", "rescore.json", "export.json", "candidate.json",
     "index.json", "comparison.json", "packet.json"],
)
@pytest.mark.parametrize("content", [b"{broken", b"[]", b'{"x":1,"x":2}', b'{"x":NaN}'])
def test_corrupt_primary_marker_never_disappears(tmp_path, filename, content):
    (tmp_path / filename).write_bytes(content)
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=tmp_path))


@pytest.mark.parametrize(
    "filename", ["run.json", "batch.json", "plan.json", "analysis.json", "manifest.json"]
)
def test_unsupported_artifacts_have_no_manifest_only_fallback(tmp_path, filename):
    (tmp_path / filename).write_bytes(
        json_bytes({"schema_version": 3, "sealed": True, "files": {}})
    )
    with pytest.raises(
        EvidenceError,
        match="unsupported_verify_artifact|invalid_|unreadable_|conflicting_verify_artifacts",
    ):
        execute(CommandRequest("verify", run=tmp_path))


@pytest.mark.parametrize(
    "pair",
    [("export.json", "rescore.json"), ("result.json", "index.json"),
     ("packet.json", "index.json"), ("candidate.json", "comparison.json")],
)
def test_conflicting_types_rejected_even_when_one_marker_is_malformed(tmp_path, pair):
    for filename in pair:
        (tmp_path / filename).write_text("{broken")
    with pytest.raises(EvidenceError, match="conflicting_verify_artifacts"):
        execute(CommandRequest("verify", run=tmp_path))


@pytest.mark.parametrize("option", ["config", "from_run", "target_run"])
def test_unrelated_options_are_input_errors(tmp_path, option):
    (tmp_path / "export.json").write_bytes(json_bytes({}))
    with pytest.raises(ContractError, match="only"):
        execute(CommandRequest("verify", run=tmp_path, **{option: tmp_path / "wrong"}))


@pytest.mark.parametrize("filename", ["index.json", "report.html"])
def test_comparison_requires_complete_report_even_if_comparison_replays(tmp_path, filename):
    _, out = make_artifact(tmp_path, "comparison")
    (out / filename).unlink()
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=out))


def test_extension_identity_survives_missing_packet(tmp_path):
    _, out = make_artifact(tmp_path, "extension")
    (out / "packet.json").unlink()
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=out))


@pytest.mark.parametrize("filename", ["packet.json", "manifest.json"])
def test_extension_symlinks_cannot_escape_artifact_root(tmp_path, filename):
    _, out = make_artifact(tmp_path, "extension")
    outside = tmp_path / "outside"
    outside.write_bytes((out / filename).read_bytes())
    (out / filename).unlink()
    symlink_or_skip(out / filename, outside)
    with pytest.raises(EvidenceError, match="unsafe_evidence_symlink"):
        execute(CommandRequest("verify", run=out))


def loaded_source(root):
    loaded = load_config(Path(__file__).parents[1] / "fixtures/config/valid.toml")
    return replace(
        loaded,
        config=Document.parse("config", read_json(root / "config.frozen.json")),
        bundle=Document.parse("bundle", read_json(root / "bundle.json")),
    )


@pytest.mark.parametrize("policy", ["public-summary.v5"])
def test_public_source_and_config_preserve_mismatch_and_readiness(tmp_path, policy):
    from inferyard.reporting.comparison_report import comparison_input
    from inferyard.reporting.public_package import payloads, projection

    root = source_fixture(tmp_path / "sources")
    data, source = comparison_input(root)
    candidate = projection(data, source, policy=policy)
    out = tmp_path / "public"
    out.mkdir()
    files = payloads(candidate)
    for name, content in files.items():
        (out / name).write_bytes(content)
    (out / "manifest.json").write_bytes(
        json_bytes(
            {
                "policy": policy,
                "candidate_id": candidate["candidate_id"],
                "files": {
                    name: hashlib.sha256(content).hexdigest() for name, content in files.items()
                },
            }
        )
    )
    loaded = loaded_source(root)
    request = CommandRequest("verify", run=out, from_run=root, config=loaded)
    code, result = execute(request)
    assert code == 0 and result.status == "matched"
    assert result.details["source_projection_verified"]
    assert result.details["declared_configuration_matches"]
    assert result.completeness == "incomplete"
    assert result.details["ready_to_run"] is False
    assert result.details["artifact_bytes_verified"] is False
    assert result.details["runtime_verified"] is False
    assert result.details["source_authenticated"] is False
    changed = loaded.config.to_dict()
    changed["conditions"]["threads"] += 1
    mismatch = replace(loaded, config=Document.parse("config", changed))
    code, result = execute(replace(request, config=mismatch))
    assert code == 3 and result.status == "mismatch"
    assert result.details["source_projection_verified"]
    assert "conditions.threads" in result.details["mismatched_fields"]
    assert result.details["ready_to_run"] is False


def test_public_source_mismatch_cannot_be_hidden_by_matching_configuration(tmp_path):
    root, out = make_artifact(tmp_path, "public")
    other = source_fixture(tmp_path / "other", model="synthetic-other")
    with pytest.raises(EvidenceError, match="source_projection_mismatch"):
        execute(CommandRequest("verify", run=out, from_run=other, config=loaded_source(root)))


@pytest.mark.parametrize(
    "binding",
    [
        "target_binding",
        "environment_binding",
        "first_event_assessments",
        "engine_rate_assessments",
        "block_gap_assessments",
    ],
)
def test_overhead_retains_specialized_refusal_exit_codes(tmp_path, monkeypatch, binding):
    marker(tmp_path, "overhead", "result.json")
    result = {"passed": True, "status": "evaluated", "limitations": []}
    if binding == "target_binding":
        result[binding] = {"applicable": False}
    elif binding == "environment_binding":
        result[binding] = {"eligible": False}
    else:
        result[binding] = {"metric": {"passed": False}}
    overhead = import_module("inferyard.application.overhead")
    monkeypatch.setattr(overhead, "read_overhead", lambda *args, **kwargs: result)
    code, checked = execute(CommandRequest("verify", run=tmp_path, target_run=tmp_path / "target"))
    assert code == 3 and checked.status == "not_passed"
    assert checked.details[binding] == result[binding]


@pytest.mark.parametrize(
    "kind,filename,value",
    [
        ("overhead", "result.json", {}),
        (
            "repeat",
            "repetition-summary.json",
            {"schema_version": 999, "kind": "frozen_repeat_summary.phase2.v1"},
        ),
        ("export", "export.json", {"format_version": True}),
    ],
)
def test_unsupported_or_malformed_marker_metadata_is_rejected(tmp_path, kind, filename, value):
    (tmp_path / filename).write_bytes(json_bytes(value))
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=tmp_path))


def test_real_incomplete_overhead_replays_and_keeps_exit_three(tmp_path):
    from inferyard.runtime.collector_overhead import assess_overhead, freeze_protocol

    protocol = freeze_protocol(
        case_ids=["fixture-case"],
        interval_ms=500,
        tolerance_ratio=0.05,
        max_wall_seconds=10,
        config_sha256="a" * 64,
        bundle_sha256="b" * 64,
        tool_source_sha256="c" * 64,
    )
    saved = assess_overhead(protocol, [])
    for name, value in (("protocol.json", protocol), ("trials.json", []), ("result.json", saved)):
        (tmp_path / name).write_bytes(json_bytes(value))
    before = digest_tree(tmp_path)
    code, result = execute(CommandRequest("verify", run=tmp_path))
    assert code == 3 and result.status == "not_passed"
    assert result.completeness == "incomplete"
    assert result.details["passed"] is False
    assert result.details["performance_comparison_eligible"] is False
    assert "four_completed_runs_required" in result.details["reasons"]
    assert before == digest_tree(tmp_path)


def test_real_repeat_summary_replays_without_requests_or_source_changes(tmp_path, config_path):
    from inferyard.config.planning import write_plan
    from inferyard.reporting.repetition_report import write_repetition_summary
    from inferyard.runtime.batch_state import open_batch
    from tests.integration.test_phase2_plan import input_package

    source = input_package(tmp_path, config_path)
    frozen = tmp_path / "frozen"
    write_plan(source, frozen)
    batch = tmp_path / "batch"
    open_batch(batch, frozen / "plan.json", diagnostic=True)
    out = tmp_path / "repeat"
    write_repetition_summary(batch, out)
    before = digest_tree(tmp_path)
    code, result = execute(CommandRequest("verify", run=out))
    assert code == 0 and result.status == "verified"
    assert result.details == {
        "verified": True,
        "analyses": 0,
        "source_runs": 0,
        "artifact_type": "repeat",
    }
    assert before == digest_tree(tmp_path)


def test_compound_result_preserves_both_limitations_and_report_failure(tmp_path, monkeypatch):
    marker(tmp_path, "comparison", "comparison.json")
    comparison = CommandResult(
        "compare-check",
        "verified",
        "complete",
        limitations=("comparison_limit",),
        details={"verified": True},
    )
    report = CommandResult(
        "report-check",
        "not_passed",
        "incomplete",
        limitations=("report_limit",),
        details={"verified": False},
    )
    monkeypatch.setattr(
        import_module("inferyard.reporting.comparison_report"),
        "execute",
        lambda request, **kwargs: (0, comparison),
    )
    monkeypatch.setattr(
        import_module("inferyard.reporting.report"),
        "execute",
        lambda request, **kwargs: (3, report),
    )
    code, result = execute(CommandRequest("verify", run=tmp_path))
    assert code == 3 and result.status == "not_passed" and result.completeness == "incomplete"
    assert result.limitations == ("comparison_limit", "report_limit")
    assert result.details["comparison"]["details"]["verified"]
    assert not result.details["report"]["details"]["verified"]


@pytest.mark.parametrize(
    "filename,value",
    [
        ("run.json", {"kind": "extension_packet.v1"}),
        ("manifest.json", {"policy": "public-summary.v5"}),
        ("parent-analysis.json", {}),
    ],
)
def test_surviving_metadata_cannot_hide_missing_primary_marker(tmp_path, filename, value):
    (tmp_path / filename).write_bytes(json_bytes(value))
    with pytest.raises(EvidenceError):
        execute(CommandRequest("verify", run=tmp_path))


def test_path_is_required_and_must_be_a_directory(tmp_path):
    with pytest.raises(ContractError, match="required"):
        execute(CommandRequest("verify"))
    file = tmp_path / "index.json"
    file.write_text("{}")
    for path in (file, tmp_path / "absent"):
        with pytest.raises(
            EvidenceError, match="directory|invalid_verify_artifact|invalid_artifact_version"
        ):
            execute(CommandRequest("verify", run=path))


@pytest.mark.parametrize("filename", ["run.json", "analysis.json"])
def test_valid_report_cannot_hide_unsupported_root_artifacts(tmp_path, filename):
    _, out = make_artifact(tmp_path, "report")
    (out / filename).write_text("{}")
    with pytest.raises(
        EvidenceError,
        match="unsupported_verify_artifact|invalid_|unreadable_|conflicting_verify_artifacts",
    ):
        execute(CommandRequest("verify", run=out))
