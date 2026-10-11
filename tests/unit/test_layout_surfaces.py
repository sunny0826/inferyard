"""External read surfaces bind synthetic layout 2 originals without changing source bytes."""

import shutil
from copy import deepcopy
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest, VerificationOptions
from inferyard.application.verification import execute
from inferyard.cli import main
from inferyard.config.loader import LoadedConfig, load_config
from inferyard.contracts.validation import Document
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, read_jsonl, sha256_file
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.reporting.comparison_report import (
    comparison_input,
    verify_comparison,
)
from inferyard.reporting.comparison_report import (
    execute as compare,
)
from inferyard.reporting.export import (
    existing_analysis,
    trial_analysis,
    verify_export,
    write_export,
)
from inferyard.reporting.public_package import verify_public, write_public
from inferyard.reporting.report import verify_report, write_report
from inferyard.reporting.rescore import verify_rescore, write_rescore
from inferyard.runtime.batch_state import remaining_budget
from inferyard.runtime.overhead_binding import bind_target, sealed_observation, workload_identity
from inferyard.runtime.runner import load_rerun
from tests.evidence_layout_helpers import build_current_original, pack_layout2, seal_layout


def tree_bytes(root):
    return {
        str(path.relative_to(root)): path.read_bytes() for path in root.rglob("*") if path.is_file()
    }


@pytest.fixture
def sources(tmp_path, monkeypatch):
    def portable_config(path):
        loaded = load_config(path)
        config = loaded.config.to_dict()
        config["engine"]["startup_args"] = ["-m", config["model"]["local_path"]]
        return replace(loaded, config=Document.parse("config", config))

    monkeypatch.setattr("inferyard.config.loader.load_config", portable_config)
    old = build_current_original(tmp_path / "legacy-original")
    new = pack_layout2(old, tmp_path / "container-original")
    return old, new


@pytest.mark.parametrize("layout", [1, 2])
@pytest.mark.parametrize("surface", ["verify", "report", "export", "rescore", "public"])
def test_external_surface_keeps_source_bytes_and_denominators(sources, tmp_path, layout, surface):
    old, new = sources
    root = old if layout == 1 else new
    before = {path: tree_bytes(path) for path in sources}
    out = tmp_path / "derived"
    data, reference = comparison_input(root)
    assert data["summary"]["counts"]["planned"] == len(data["selection"]["case_ids"])
    assert data["summary"]["counts"]["completed"] == len(data["selection"]["case_ids"])
    if surface == "verify":
        code, result = execute(CommandRequest("verify", run=root))
        assert code == 0 and result.details["artifact_type"] == "run"
    elif surface == "report":
        index = write_report([root], out)
        assert index["report_format_version"] == 8
        assert {p.name for p in out.iterdir()} == {
            "index.json",
            "report.html",
            "artifact-manifest.json",
        }
        assert verify_report(out, options=VerificationOptions(rerender=True))["render_checked"]
        expected = {"manifest.json", "events.jsonl"} | (
            {"inputs.json"} if layout == 2 else {"config.frozen.json", "plan.json"}
        )
        assert set(index["runs"][0]["evidence"]) == expected
    elif surface == "export":
        write_export(out, run=root)
        assert verify_export(out)["verified"]
    elif surface == "rescore":
        analysis = write_rescore(
            root, out, scorer_id="phase2.v2", reason="synthetic layout regression"
        )
        assert analysis["source_runs"][0]["manifest_sha256"] == reference["manifest_sha256"]
        assert verify_rescore(out)["verified"]
    else:
        write_public(root, out)
        assert verify_public(out, root)["source_projection_verified"]
    assert before == {path: tree_bytes(path) for path in sources}


@pytest.mark.parametrize(
    "damage,expected", [("unknown", 2), ("mixed", 4), ("conflict", 4), ("hash", 4)]
)
def test_verify_layout_version_and_conflicting_evidence_exit_codes(sources, damage, expected):
    old, new = sources
    inputs = read_json(new / "inputs.json")
    if damage == "unknown":
        inputs["evidence_layout_version"] = 99
        (new / "inputs.json").write_bytes(json_bytes(inputs))
        manifest = read_json(new / "manifest.json")
        manifest["evidence_layout_version"] = 99
        manifest["files"]["inputs.json"] = {
            "bytes": (new / "inputs.json").stat().st_size,
            "sha256": sha256_file(new / "inputs.json"),
            "derived": False,
        }
        (new / "manifest.json").write_bytes(json_bytes(manifest))
    elif damage == "mixed":
        (new / "run.json").write_bytes((old / "run.json").read_bytes())
    elif damage == "conflict":
        manifest = read_json(new / "manifest.json")
        manifest["evidence_layout_version"] = 1
        (new / "manifest.json").write_bytes(json_bytes(manifest))
    else:
        inputs["documents"]["config"]["sha256"] = "0" * 64
        (new / "inputs.json").write_bytes(json_bytes(inputs))
        seal_layout(new, inputs["documents"]["run"]["payload"])
    before = tree_bytes(new)
    assert main(["verify", "--path", str(new)]) == expected
    assert tree_bytes(new) == before


def test_analysis_checks_both_container_and_member_hash(sources, tmp_path):
    _, new = sources
    out = tmp_path / "analysis-input"
    out.mkdir()
    shutil.copytree(new, out / "run")
    analysis, _ = trial_analysis(new)
    for metric in analysis["metrics"]:
        for ref in metric["evidence_refs"]:
            ref["path"] = "run/" + ref["path"]
    path = out / "analysis.json"
    path.write_bytes(json_bytes(analysis))
    assert existing_analysis(path)[0] == analysis
    broken = deepcopy(analysis)
    ref = next(
        ref for metric in broken["metrics"] for ref in metric["evidence_refs"] if "member" in ref
    )
    assert ref["sha256"] == sha256_file(out / ref["path"])
    assert ref["sha256"] != ref["member"]["sha256"]
    ref["member"]["sha256"] = "0" * 64
    path.write_bytes(json_bytes(broken))
    with pytest.raises(EvidenceError):
        existing_analysis(path)


def test_rerun_loads_actual_embedded_source_without_virtual_path(sources, tmp_path, monkeypatch):
    old, _ = sources
    copied = tmp_path / "legacy-with-source"
    shutil.copytree(old, copied)
    raw = b"# exact redacted bytes\r\nschema_version = 3\r\n"
    (copied / "config.input.toml").write_bytes(raw)
    manifest = read_json(copied / "manifest.json")
    manifest["files"]["config.input.toml"] = {
        "sha256": sha256_file(copied / "config.input.toml"),
        "bytes": len(raw),
        "derived": False,
    }
    (copied / "manifest.json").write_bytes(json_bytes(manifest))
    root = pack_layout2(copied, tmp_path / "embedded-source")
    evidence = TrialEvidence(root)
    old_pid = evidence.input("config")["endpoint"]["server_pid"]

    def ticks(pid):
        if pid == old_pid:
            from inferyard.platforms.identity import PreflightError

            raise PreflightError("service_process_unavailable")
        return 98765

    monkeypatch.setattr("inferyard.runtime.runner.process_start_ticks", ticks)
    before = tree_bytes(root)
    loaded, _ = load_rerun(
        CommandRequest(
            "run", from_run=root, server_pid=old_pid + 1, endpoint_url="http://127.0.0.1:9090"
        )
    )
    assert loaded.source is None
    assert loaded.configuration_source() == {
        "data": raw,
        "media_type": "application/toml",
        "missing_reason": None,
    }
    assert tree_bytes(root) == before


def test_loaded_configuration_preserves_file_and_missing_original(config_path):
    loaded = load_config(config_path)
    assert loaded.source == Path(config_path).resolve()
    assert loaded.configuration_source()["data"] == Path(config_path).read_bytes()
    missing = LoadedConfig(
        None, loaded.config, loaded.bundle, source_missing_reason="not_persisted"
    )
    assert missing.configuration_source() == {
        "data": None,
        "media_type": None,
        "missing_reason": "not_persisted",
    }


def test_remaining_budget_reads_saved_member_and_reserves_unknown_crash(sources, tmp_path):
    old, _ = sources
    copied = tmp_path / "budget-source"
    shutil.copytree(old, copied)
    evidence = TrialEvidence(old)
    run, plan = evidence.input("run"), evidence.input("plan")
    payload = {"run_id": run["run_id"], "elapsed_seconds": 2.5}
    path = copied / "execution-budget.json"
    path.write_bytes(json_bytes(payload))
    manifest = read_json(copied / "manifest.json")
    manifest["files"][path.name] = {
        "sha256": sha256_file(path),
        "bytes": path.stat().st_size,
        "derived": False,
    }
    (copied / "manifest.json").write_bytes(json_bytes(manifest))
    packed = pack_layout2(copied, tmp_path / "budget-container")
    before = tree_bytes(packed)
    budget = plan["experiment"]["budget"]["max_wall_seconds"]
    data = {"path": packed, "run": run}
    assert remaining_budget(plan, [data]) == budget - 2.5
    assert before == tree_bytes(packed)
    (packed / "manifest.json").unlink()
    reserved = plan["trials"][0]["total_budget_seconds"]
    assert remaining_budget(plan, [data]) == max(0, budget - reserved)


def test_legacy_budget_file_outside_manifest_is_rejected(sources, tmp_path):
    old, _ = sources
    copied = tmp_path / "legacy-unsealed-budget"
    shutil.copytree(old, copied)
    evidence = TrialEvidence(copied)
    run, plan = evidence.input("run"), evidence.input("plan")
    (copied / "execution-budget.json").write_bytes(
        json_bytes({"run_id": run["run_id"], "elapsed_seconds": 2.5})
    )
    before = tree_bytes(copied)
    with pytest.raises(EvidenceError, match="budget_evidence_not_sealed"):
        remaining_budget(plan, [{"path": copied, "run": run}])
    assert tree_bytes(copied) == before


def test_optional_observation_is_never_inferred_from_container_existence(sources):
    _, packed = sources
    evidence = TrialEvidence(packed)
    assert evidence.observation("collector", stage="prepare") is None
    with pytest.raises(EvidenceError):
        sealed_observation(evidence, "collector")
    start = sealed_observation(evidence, "environment", stage="start")
    assert start == evidence.observation("environment", stage="start")


def test_mixed_layout_comparison_and_report_resolve_moved_sources(sources, tmp_path):
    old, _ = sources
    parent = tmp_path / "portable-family"
    parent.mkdir()
    left = parent / "left"
    shutil.copytree(old, left)
    other = build_current_original(tmp_path / "other-original")
    right = pack_layout2(other, parent / "right")
    out = parent / "comparison"
    before = {root: tree_bytes(root) for root in (left, right)}
    code, result = compare(CommandRequest("compare", left=left, right=right, out=out))
    assert (
        code == 0
        and result.details["source_runs"][0]["run_id"] != result.details["source_runs"][1]["run_id"]
    )
    assert verify_comparison(out)["verified"]
    assert verify_report(out)["sources_verified"]
    assert before == {root: tree_bytes(root) for root in (left, right)}
    moved = tmp_path / "moved-family"
    shutil.copytree(parent, moved)
    shutil.rmtree(parent)
    assert verify_comparison(moved / "comparison")["verified"]
    assert verify_report(moved / "comparison", options=VerificationOptions(rerender=True))[
        "render_checked"
    ]


def test_target_binding_does_not_downgrade_corrupt_members_to_non_applicability(sources):
    _, root = sources
    data, _ = comparison_input(root)
    evidence = TrialEvidence(root)
    protocol = {
        "kind": "collector_overhead_abba.v2",
        "config_sha256": evidence.input_digest("config"),
        "bundle_sha256": evidence.input_digest("bundle"),
        "tool_source_sha256": data["run"]["tool_source_sha256"],
        "case_ids": data["selection"]["case_ids"],
        "observer": {},
        "protocol_sha256": "a" * 64,
    }
    rows, _ = read_jsonl(root / "observations.jsonl")
    rows[0]["payload_sha256"] = "0" * 64
    (root / "observations.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(root, data["run"])
    with pytest.raises(EvidenceError):
        bind_target(root, data, protocol, workload_identity(data), [], {"passed": False}, {})
