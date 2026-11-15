"""Diagnostic SVG generation through the existing synthetic transport and real journal."""

import asyncio
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.contracts.validation import Document
from inferyard.evidence.storage import read_json, sha256_file
from inferyard.reporting.studio_report import write_studio_report
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario
SVG = (
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200">'
    "<style>@keyframes pedal{to{transform:rotate(360deg)}}"
    ".wheel{animation:pedal 2s linear infinite;transform-origin:center}</style>"
    '<circle class="wheel" cx="100" cy="120" r="50" fill="orange"/></svg>'
)


def svg_inputs(scenario):
    request, deps, calls, settings = scenario
    bundle = read_json(Path("bundles/zh-svg-pelican.json"))
    config = request.config.config.to_dict()
    config["bundle"].update(
        version=bundle["version"], path=str(Path("bundles/zh-svg-pelican.json").resolve())
    )
    loaded = replace(
        request.config,
        config=Document.parse("config", config),
        bundle=Document.parse("bundle", bundle),
    )
    return inputs((replace(request, config=loaded), deps, calls, settings))


@pytest.mark.parametrize(
    "answer,finish", [(SVG, "stop"), ("<svg><path", "length")]
)
def test_diagnostic_svg_studio_projection_is_not_scored(scenario, tmp_path, answer, finish):
    calls = scenario[2]
    scenario[3].update(answer=answer, finish_reason=finish)
    plan, loaded, deps, output = svg_inputs(scenario)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    assert data["summary"]["counts"]["completed"] == 1
    assert data["requests"][0]["score"]["quality_state"] == "not_applicable"
    assert data["summary"]["quality"]["Q01"] == {}
    metrics = data["summary"]["metric_observations"]
    assert not any(m["metric_id"].startswith("Q") for m in metrics)
    assert any(m["metric_id"] == "L03" and m["value"] is not None for m in metrics)
    before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    call_count = len(calls)
    out = tmp_path / "svg-report"
    document = write_studio_report([root], out)
    run = document["runs"][0]
    (request,) = run["requests"]
    assert request["case_id"] == "svg-pelican-01"
    assert request["quality"] == "not_applicable"
    assert request["category_label"] == data["requests"][0]["category"]
    raw = (out / "studio.json").read_text()
    assert "svg_gallery" not in raw and "svg_view" not in raw and "contents" not in raw
    assert SVG not in raw
    assert [row["case_id"] for row in run["requests"]] == [
        row["case_id"] for row in data["requests"]
    ]
    assert len(calls) == call_count
    assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}


def test_failed_svg_preserves_execution_failure_without_quality_score(scenario, tmp_path):
    scenario[3]["fail_index"] = 5  # After two probes and three warmups.
    plan, loaded, deps, output = svg_inputs(scenario)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    assert data["summary"]["counts"]["failed"] == 1
    assert data["requests"][0]["quality_state"] == "not_applicable"
    assert data["summary"]["quality"]["Q01"] == {}
    assert data["summary"]["input_lengths"]["bins"][0]["quality_rate"] is None
    out = tmp_path / "failed-report"
    document = write_studio_report([root], out)
    assert document["runs"][0]["requests"][0]["quality"] is None
