"""Public plan/resume flows keep frozen inputs, failure history and service boundaries."""

import asyncio
import copy
import hashlib
import json
import sys
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case
from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.config.planning import write_plan
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, sha256_file
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.batch_runner import execute_async
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.test_phase2_contracts import experiment

scenario = runner_scenario


def batch(
    tmp_path,
    scenario,
    *,
    second=False,
    duration_limit=None,
    capacity=False,
    performance_policy=None,
):
    request, deps, _, _ = scenario
    deps.scorer = score_case
    inputs = tmp_path / "inputs"
    inputs.mkdir()
    config, corpus = request.config.config.to_dict(), request.config.bundle.to_dict()
    config["bundle"]["path"] = "bundle.json"
    definition = experiment()
    if performance_policy is not None:
        definition["performance_environment"] = performance_policy
    definition["definition_versions"] = dict.fromkeys(
        ["measurement", "scoring", "comparison"], "phase2.v1"
    )
    definition["budget"]["max_wall_seconds"] = 100_000
    workload = definition["workloads"][0]
    workload["timeout_seconds"] = config["execution"]["timeout_seconds"]
    workload["protocol"]["case_ids"] = [c["case_id"] for c in corpus["cases"]]
    if duration_limit is not None:
        workload["purpose"] = "stability"
        workload["repeats"] = 1
        workload["protocol"].update(
            kind="duration",
            duration_seconds=0.5,
            max_requests=duration_limit,
            window_seconds=0.5,
            min_completed_per_case_per_window=1,
            drain_timeout_seconds=workload["timeout_seconds"],
        )
    if capacity:
        definition["capacity_stop"] = "first_failed_request"
        workload.update(purpose="performance", input_target_tokens=1)
    if second:
        workload["repeats"] = 1
    for kind, data in [("config", config), ("bundle", corpus)]:
        raw = json.dumps(data).encode()
        (inputs / f"{kind}.json").write_bytes(raw)
        workload[kind] = dict(path=f"{kind}.json", sha256=hashlib.sha256(raw).hexdigest())
    if second:
        definition["workloads"].append({**copy.deepcopy(workload), "workload_id": "w2"})
    source = inputs / "experiment.json"
    source.write_text(json.dumps(definition))
    frozen = tmp_path / "frozen"
    write_plan(source, frozen)
    return CommandRequest(
        "run", frozen_plan=frozen / "plan.json", output_root=tmp_path / "batch", diagnostic=True
    ), deps


def test_three_repetitions_new_runs_then_no_implicit_rerun(tmp_path, scenario):
    request, deps = batch(tmp_path, scenario)
    scenario[3]["fail_index"] = 6
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0 and result.status == "finished"
    assert result.completeness == "incomplete"  # diagnostic label is never promoted
    assert len(scenario[2]) == 24
    paths = [Path(p) for p in result.details["runs"]]
    data = [read_trial(p) for p in paths]
    assert [d["run"]["relation"] for d in data] == ["initial", "repeat", "repeat"]
    assert data[0]["summary"]["counts"]["failed"] == 1
    assert data[1]["run"]["parent_run_id"] == data[0]["run"]["run_id"]
    assert all((p / "execution-budget.json").exists() for p in paths)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0 and len(scenario[2]) == 24


def test_cancel_resume_only_unexecuted_then_next_planned_repetitions(
    tmp_path, scenario, monkeypatch
):
    request, deps = batch(tmp_path, scenario)

    async def cancel():
        task = asyncio.create_task(execute_async(request, deps))
        while len(scenario[2]) < 6 and not task.done():
            await asyncio.sleep(0.001)
        task.cancel()
        return await task

    code, result = asyncio.run(cancel())
    assert code == 130 and len(scenario[2]) == 6
    parent_path = Path(result.details["runs"][0])
    hashes = {p.name: sha256_file(p) for p in parent_path.iterdir()}
    from inferyard.application.verification import execute as verify
    from inferyard.runtime import batch_state

    with monkeypatch.context() as patch:
        patch.setattr(
            batch_state, "tool_source_hash", lambda: pytest.fail("offline current source")
        )
        patch.setattr(batch_state, "open_batch", lambda *a, **k: pytest.fail("offline admission"))
        checked_code, checked = verify(CommandRequest("verify", run=request.output_root))
    assert checked_code == 0 and checked.status == "verified"
    assert checked.completeness == "incomplete"
    assert checked.details["sealed"] is False  # No whole-batch seal.
    assert len(checked.details["missing_trials"]) == 2
    assert checked.details["trials"][0]["sealed"]
    assert checked.details["trials"][0]["execution_completeness"] == "incomplete"
    assert checked.details["trials"][0]["stop_reason"] == "user_cancelled"
    manifest = parent_path / "manifest.json"
    raw_manifest = manifest.read_bytes()
    manifest.unlink()
    assert verify(CommandRequest("verify", run=request.output_root))[0] == 3
    manifest.write_bytes(raw_manifest)
    code, blocked = asyncio.run(execute_async(request, deps))
    assert code == 3 and blocked.status == "resume_required" and len(scenario[2]) == 6
    resume = CommandRequest("resume", from_run=parent_path)
    code, resumed = asyncio.run(execute_async(resume, deps))
    assert code == 0 and resumed.status == "resumed_subset"
    assert len(scenario[2]) == 13  # five non-formal + exactly two remaining cases
    child = read_trial(Path(resumed.details["runs"][-1]))
    assert child["selection"]["case_ids"] == read_trial(parent_path)["selection"]["case_ids"][1:]
    assert child["summary"]["scope_complete"] and child["summary"]["completeness"] == "incomplete"
    assert hashes == {p.name: sha256_file(p) for p in parent_path.iterdir()}
    with pytest.raises(PreflightError, match="latest_unfinished"):
        asyncio.run(execute_async(resume, deps))
    code, done = asyncio.run(execute_async(request, deps))
    assert code == 0 and len(scenario[2]) == 29
    assert len(done.details["runs"]) == 4


def test_workload_handoff_cold_policy_requires_binding_and_new_process(
    tmp_path, scenario, monkeypatch
):
    import inferyard.runtime.batch_state as state

    config = scenario[0].config.config.to_dict()
    config["execution"]["require_fresh_process"] = True
    scenario = (
        replace(
            scenario[0], config=replace(scenario[0].config, config=Document.parse("config", config))
        ),
        *scenario[1:],
    )
    request, deps = batch(tmp_path, scenario, second=True)
    code, first = asyncio.run(execute_async(request, deps))
    assert code == 3 and first.status == "awaiting_handoff" and len(scenario[2]) == 8
    code, waiting = asyncio.run(execute_async(request, deps))
    assert code == 3 and waiting.status == "awaiting_handoff" and len(scenario[2]) == 8
    old = scenario[0].config.config.to_dict()["endpoint"]
    same = replace(
        request,
        workload_id="w2",
        server_pid=old["server_pid"],
        endpoint_url=old["url"],
        handoff_note="synthetic operator handoff",
    )
    monkeypatch.setattr(state, "process_start_ticks", lambda pid: old["process_start_ticks"])
    with pytest.raises(PreflightError, match="new_service_process"):
        asyncio.run(execute_async(same, deps))
    new_pid = old["server_pid"] + 100_000

    def ticks(pid):
        if pid == old["server_pid"]:
            raise PreflightError("service_process_unavailable")
        assert pid == new_pid
        return 555

    monkeypatch.setattr(state, "process_start_ticks", ticks)
    code, final = asyncio.run(execute_async(replace(same, server_pid=new_pid), deps))
    assert code == 0 and len(scenario[2]) == 16
    binding = json.loads((Path(final.details["runs"][-1]) / "service-binding.json").read_text())
    assert binding["effective_endpoint"]["server_pid"] == new_pid


def test_batch_has_its_own_frozen_sources(tmp_path, scenario):
    request, deps = batch(tmp_path, scenario)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    (tmp_path / "inputs").rename(tmp_path / "removed-inputs")
    (tmp_path / "frozen").rename(tmp_path / "removed-frozen")
    request = replace(request, frozen_plan=request.output_root / "frozen-plan/plan.json")
    assert asyncio.run(execute_async(request, deps))[0] == 0
    assert len(scenario[2]) == 24


def test_batch_identity_changes_block_without_generation(tmp_path, scenario):
    request, deps = batch(tmp_path, scenario)
    assert asyncio.run(execute_async(request, deps))[0] == 0
    with pytest.raises(EvidenceError, match="identity_or_tool_changed"):
        asyncio.run(execute_async(replace(request, diagnostic=False), deps))
    assert len(scenario[2]) == 24


@pytest.mark.skipif(
    sys.platform not in ("linux", "darwin"), reason="Public Linux/macOS live dispatch"
)
def test_cli_plan_dispatch_stdout_single_json_and_progress_stderr(
    tmp_path, scenario, monkeypatch, capsys
):
    import inferyard.runtime.batch_runner as runner

    request, deps = batch(tmp_path, scenario)
    monkeypatch.setattr(runner, "TrialDependencies", lambda **kwargs: deps)
    # Keep the mock transport while exercising the real CLI and orchestration.
    monkeypatch.setattr(runner, "adapter_factory", lambda identifier: deps.adapter)
    monkeypatch.setattr(runner, "collector_factory", lambda identifier: deps.sampler)
    code = main(
        [
            "run",
            "--plan",
            str(request.frozen_plan),
            "--output-root",
            str(request.output_root),
            "--diagnostic",
        ]
    )
    output = capsys.readouterr()
    assert code == 0 and len(output.out.splitlines()) == 1
    assert json.loads(output.out)["status"] == "finished"
    assert output.err.count("trial ") == 3


@pytest.mark.parametrize(
    "args",
    [
        ["run", "--plan", "plan.json"],
        ["run", "--config", "x", "--workload", "w1"],
        ["resume", "--from-run", "x", "--server-pid", "123"],
        [
            "resume",
            "--from-run",
            "x",
            "--endpoint-url",
            "https://external.invalid",
            "--server-pid",
            "123",
        ],
    ],
)
def test_invalid_new_cli_combinations_rejected_before_backend(args, capsys):
    assert main(args) == 2
    assert json.loads(capsys.readouterr().out)["status"] == "blocked"


def test_duration_batch_finishes_without_implicit_repeat(tmp_path, scenario):
    request, deps = batch(tmp_path, scenario, duration_limit=20)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    trial = read_trial(Path(result.details["runs"][0]))
    assert trial["summary"]["duration"]["window_completed"]
    count = len(scenario[2])
    code, _ = asyncio.run(execute_async(request, deps))
    assert code == 0 and len(scenario[2]) == count


def test_incomplete_duration_cannot_implicitly_resume(tmp_path, scenario):
    request, deps = batch(tmp_path, scenario, duration_limit=1)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3
    count = len(scenario[2])
    for command in (request, CommandRequest("resume", from_run=Path(result.details["runs"][0]))):
        code, blocked = asyncio.run(execute_async(command, deps))
        assert code == 3 and blocked.status == "duration_requires_new_window"
        assert len(scenario[2]) == count


def test_repeat_summary_is_offline_and_preserves_sources(tmp_path, scenario):
    from inferyard.reporting.repetition_report import (
        verify_repetition_summary,
        write_repetition_summary,
    )

    request, deps = batch(tmp_path, scenario)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    assert result.details["repetition_metrics"][0]["S03"]["value"] is None  # diagnostic
    before = {str(p): sha256_file(p) for p in request.output_root.rglob("*") if p.is_file()}
    calls = len(scenario[2])
    result = write_repetition_summary(request.output_root, tmp_path / "repeat-report")
    assert len(result["source_runs"]) == 3
    assert verify_repetition_summary(tmp_path / "repeat-report")["analyses"] == 1
    assert result["groups"][0]["S03"]["value"] is None
    assert result["groups"][0]["S04"]["range_ms"] is None
    assert len(scenario[2]) == calls
    assert before == {str(p): sha256_file(p) for p in request.output_root.rglob("*") if p.is_file()}


def test_repeat_analysis_tampering_rejected(tmp_path, scenario):
    from inferyard.reporting.repetition_report import (
        verify_repetition_summary,
        write_repetition_summary,
    )

    request, deps = batch(tmp_path, scenario)
    asyncio.run(execute_async(request, deps))
    out = tmp_path / "repeat-analysis"
    result = write_repetition_summary(request.output_root, out)
    assert verify_repetition_summary(out)["verified"]
    path = out / result["analysis_files"][0]
    data = json.loads(path.read_text())
    assert all(m["run_id"] is None and m["trial_id"] is None for m in data["metrics"])
    assert all(len(m["source_run_ids"]) == 3 for m in data["metrics"])
    data["metrics"][0]["limitations"].append("tampered")
    path.write_text(json.dumps(data))
    with pytest.raises(EvidenceError, match="recomputation|presentation_bytes_changed"):
        verify_repetition_summary(out)


def test_comparison_reads_saved_trials_without_requests(tmp_path, scenario):
    from inferyard.reporting.comparison_report import execute, verify_comparison

    request, deps = batch(tmp_path, scenario)
    _, batch_result = asyncio.run(execute_async(request, deps))
    paths = [Path(p) for p in batch_result.details["runs"]]
    calls = len(scenario[2])
    before = {str(p): sha256_file(p) for root in paths for p in root.iterdir() if p.is_file()}
    code, result = execute(
        CommandRequest("compare", left=paths[0], right=paths[1], out=tmp_path / "comparison")
    )
    assert code == 0
    assert result.details["eligibility"] == {
        "quality": False,
        "completion": False,
        "performance": False,
    }
    assert result.details["completion_rate_difference"] is None
    assert result.details["difference_direction"] == "right_minus_left"
    assert len(result.details["sides"]) == 2
    out = tmp_path / "comparison"
    assert verify_comparison(out)["verified"]
    saved = json.loads((out / "comparison.json").read_text())
    saved["completion_rate_difference"] = 1
    (out / "comparison.json").write_text(json.dumps(saved))
    with pytest.raises(EvidenceError, match="recomputation|presentation_bytes_changed"):
        verify_comparison(out)
    assert len(scenario[2]) == calls
    assert before == {
        str(p): sha256_file(p) for root in paths for p in root.iterdir() if p.is_file()
    }


def test_candidate_filter_public_cli_is_offline_and_empty_is_valid(tmp_path, scenario, capsys):
    from inferyard.analysis.candidate_filter import filter_candidates

    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    paths = result.details["runs"]
    spec = {
        "version": 1,
        "reference": paths[0],
        "candidates": paths,
        "constraints": [
            {
                "metric_id": "Q01",
                "statistic": "pass_rate",
                "category": "qa",
                "error_category": None,
                "source": "scores",
                "unit": "ratio",
                "definition_version": "phase2.v1",
                "operator": ">=",
                "threshold": 0.5,
            }
        ],
    }
    spec_path = tmp_path / "filter.json"
    spec_path.write_text(json.dumps(spec))
    calls = len(scenario[2])
    before = {str(p): sha256_file(p) for p in request.output_root.rglob("*") if p.is_file()}
    out = tmp_path / "filtered"
    assert main(["filter-candidates", "--spec", str(spec_path), "--out", str(out)]) == 0
    capsys.readouterr()
    saved = json.loads((out / "candidates.json").read_text())
    assert saved["matched_run_ids"] == []  # Diagnostic runs cannot qualify.
    assert len(saved["candidates"]) == 3
    spec["candidates"] = []
    assert filter_candidates(spec, tmp_path)["candidates"] == []
    assert len(scenario[2]) == calls
    assert before == {str(p): sha256_file(p) for p in request.output_root.rglob("*") if p.is_file()}


def test_report_is_offline_escaped_and_preserves_denominators(tmp_path, scenario):
    from inferyard.reporting.report import _environment, verify_report, write_report

    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    paths = [Path(p) for p in result.details["runs"]]
    calls = len(scenario[2])
    before = {str(p): sha256_file(p) for root in paths for p in root.iterdir() if p.is_file()}
    index = write_report(paths, tmp_path / "report")
    assert len(index["runs"]) == 3
    report_root = tmp_path / "report"
    assert verify_report(report_root)["semantic_verified"]
    html_path = report_root / "report.html"
    original_html = html_path.read_bytes()
    html_path.write_bytes(original_html + b"tampered")
    with pytest.raises(EvidenceError, match="html_recomputation|presentation_bytes_changed"):
        verify_report(report_root)
    html_path.write_bytes(original_html)
    index_path = report_root / "index.json"
    saved = json.loads(index_path.read_text())
    saved["runs"][0]["summary"]["completion_rate"]["value"] = 99
    index_path.write_text(json.dumps(saved))
    with pytest.raises(EvidenceError, match="index_recomputation|presentation_bytes_changed"):
        verify_report(report_root)
    for run in index["runs"]:
        assert run["summary"]["completion_rate"]["denominator"] == 3
        assert run["schema_version"] == 3
    index["runs"][0]["model"] = "<script>window.pwned=true</script>"
    index["runs"][0]["requests"][0]["content"] = '<img src=x onerror="window.pwned=true">'
    html = _environment().get_template("report.html").render(index=index)
    assert "<script>window.pwned" not in html
    assert "&lt;script&gt;window.pwned" in html
    assert "<img src=x" not in html
    assert len(scenario[2]) == calls
    assert before == {
        str(p): sha256_file(p) for root in paths for p in root.iterdir() if p.is_file()
    }
    with pytest.raises(EvidenceError, match="duplicate"):
        write_report([paths[0], paths[0]], tmp_path / "duplicate-report")
    with pytest.raises(EvidenceError, match="inside_original"):
        write_report([paths[0]], paths[0] / "report")


def test_export_replays_formats_and_never_changes_source(tmp_path, scenario):
    from inferyard.reporting.export import verify_export, write_export

    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    root = Path(result.details["runs"][0])
    before = {str(p): sha256_file(p) for p in root.iterdir() if p.is_file()}
    calls = len(scenario[2])
    out = tmp_path / "export"
    manifest = write_export(out, run=root)
    verified = verify_export(out)
    assert verified["analysis_id"] == manifest["analysis_id"]
    assert verified["metric_rows"] > 0
    csv_path = out / "metrics.csv"
    csv_path.write_text(csv_path.read_text() + "forged\n")
    with pytest.raises(EvidenceError, match="recomputation|presentation_bytes_changed"):
        verify_export(out)
    assert len(scenario[2]) == calls
    assert before == {str(p): sha256_file(p) for p in root.iterdir() if p.is_file()}


@pytest.mark.parametrize("revision", ["phase2.v1", "phase2.v2"])
def test_rescore_is_separate_replayable_analysis_with_export(tmp_path, scenario, revision):
    from inferyard.reporting.export import verify_export, write_export
    from inferyard.reporting.rescore import verify_rescore, write_rescore

    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    root = Path(result.details["runs"][0])
    before = {str(p): sha256_file(p) for p in root.iterdir() if p.is_file()}
    calls = len(scenario[2])
    out = tmp_path / "rescored"
    analysis = write_rescore(root, out, scorer_id=revision, reason="offline verification")
    assert analysis["analysis_id"] != analysis["parent_analysis_id"]
    assert analysis["definition_versions"]["scoring"] == revision
    assert all(m["metric_id"].startswith("Q") for m in analysis["metrics"])
    assert verify_rescore(out)["requests"] == 3
    write_export(tmp_path / "rescore-export", analysis_path=out / "analysis.json")
    assert verify_export(tmp_path / "rescore-export")["analysis_id"] == analysis["analysis_id"]
    changed = json.loads((out / "analysis.json").read_text())
    changed["metrics"][0]["limitations"].append("forged")
    (out / "analysis.json").write_text(json.dumps(changed))
    with pytest.raises(EvidenceError, match="recomputation|presentation_bytes_changed"):
        verify_rescore(out)
    with pytest.raises(EvidenceError, match="unsupported_rescore"):
        write_rescore(root, tmp_path / "unknown", scorer_id="unknown", reason="test")
    assert len(scenario[2]) == calls
    assert before == {str(p): sha256_file(p) for p in root.iterdir() if p.is_file()}
