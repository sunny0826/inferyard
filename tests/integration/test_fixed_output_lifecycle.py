import asyncio
from dataclasses import replace

import pytest

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.request_snapshots import snapshot_name_for_event
from inferyard.evidence.storage import (
    EvidenceError,
    json_bytes,
    read_json,
    read_jsonl,
    sha256_file,
)
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario
from tests.integration.test_trial_runner import inputs

__all__ = ["scenario"]


def strict_inputs(scenario):
    plan, loaded, deps, out = inputs(scenario)
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    config["generation"].update(max_tokens=2, stop=[])
    bundle.update(schema_version=3, task_protocol="performance", review_records=[])
    for case in bundle["cases"]:
        case.update(category="performance", rules={"output_target_tokens": 2})
    loaded = replace(
        loaded, config=Document.parse("config", config), bundle=Document.parse("bundle", bundle)
    )
    source = plan["experiment"]
    source["workloads"][0].update(
        purpose="performance", output_budget_tokens=2, output_mode="strict_fixed_length"
    )
    scenario[3]["finish_reason"] = "length"
    return compile_plan(source), loaded, deps, out


@pytest.mark.parametrize("diagnostic", [True, False])
@pytest.mark.parametrize("tamper", ["parameters", "request", "unsealed_engine", None])
def test_strict_diagnostic_replays_requests_parameters_and_missing_formal_acceptance(
    scenario, monkeypatch, diagnostic, tamper
):
    plan, loaded, deps, out = strict_inputs(scenario)
    # Synthetic lifecycle only: this bypass is not a human corpus review record.
    if not diagnostic:
        monkeypatch.setattr("inferyard.runtime.trial_runner.require_review", lambda bundle: None)
    code, data, root = asyncio.run(
        run_trial(
            plan,
            plan["trials"][0]["trial_id"],
            loaded,
            out,
            diagnostic=diagnostic,
            dependencies=deps,
        )
    )
    assert code == 0 and all(b["ignore_eos"] is True for b in scenario[2])
    assert all(r["quality_state"] == "not_applicable" for r in data["requests"])
    m = next(
        m
        for m in data["summary"]["metric_observations"]
        if m["statistic"] == "strict_fixed_length_target_rate"
    )
    assert m["numerator"] == m["denominator"] == 3
    assert m["value"] == (None if diagnostic else 1.0)
    assert read_trial(root)["summary"] == data["summary"]
    if tamper is None:
        from inferyard.reporting.report import verify_report, write_report

        report = root.parent / "strict-report"
        before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
        index = write_report([root], report)
        rows = index["runs"][0]["scan_view"]["strict_output_targets"]
        assert len(rows) == 1 and rows[0]["value"] == m["value"]
        assert 'class="strict-output"' in (report / "report.html").read_text()
        assert verify_report(report)["verified"]
        assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
        return
    if tamper == "unsealed_engine":
        manifest = read_json(root / "manifest.json")
        del manifest["files"]["service.props.json"]
        (root / "manifest.json").write_bytes(json_bytes(manifest))
        with pytest.raises(EvidenceError, match="strict_output_engine_evidence_unsealed"):
            read_trial(root)
        return
    # Even with repaired file hashes, an altered parameter snapshot fails semantic replay.
    first_start = next(
        event
        for event in read_jsonl(root / "events.jsonl")[0]
        if event["event_type"] == "request_started"
    )
    p = root / (
        "effective-stream.json"
        if tamper == "parameters"
        else snapshot_name_for_event(root, first_start)
    )
    effective = read_json(p)
    if tamper == "parameters":
        effective["slots"][0]["params"]["ignore_eos"] = False
    else:
        effective["ignore_eos"] = False
    p.write_bytes(json_bytes(effective))
    manifest = read_json(root / "manifest.json")
    manifest["files"][p.name].update(sha256=sha256_file(p), bytes=p.stat().st_size)
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    with pytest.raises(EvidenceError, match="strict_output_"):
        read_trial(root)


def test_strict_probe_refusal_leaves_every_formal_case_unexecuted(scenario):
    plan, loaded, deps, out = strict_inputs(scenario)
    scenario[3]["finish_reason"] = "stop"
    code, data, _ = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, out, diagnostic=True, dependencies=deps
        )
    )
    assert code != 0 and len(scenario[2]) == 1
    assert data["summary"]["counts"]["not_executed"] == 3
    assert data["summary"]["stop_reason"] == "strict_output_probe_not_verified"
