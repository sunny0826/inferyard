"""Diagnostic SVG generation through the existing synthetic transport and real journal."""

import asyncio
import base64
import hashlib
import re
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import json_bytes, read_json, sha256_file
from inferyard.reporting.report import _environment, build_index, verify_report, write_report
from inferyard.reporting.report_assets import template_hash, template_name
from inferyard.reporting.report_common import render_report_html
from inferyard.runtime.trial_runner import run_trial
from tests.helpers import fixture_run
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario
SVG = (
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200">'
    "<style>@keyframes pedal{to{transform:rotate(360deg)}}"
    ".wheel{animation:pedal 2s linear infinite;transform-origin:center}</style>"
    '<circle class="wheel" cx="100" cy="120" r="50" fill="orange"/></svg>'
)


def verify_artifact(path):
    code, result = execute(CommandRequest("verify", run=path))
    assert code == 0
    return result.details


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
    "answer,status,finish", [(SVG, "ok", "stop"), ("<svg><path", "not_found", "length")]
)
def test_diagnostic_svg_report_and_offline_verification(scenario, tmp_path, answer, status, finish):
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
    assert read_trial(root)["summary"] == data["summary"]
    before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    call_count = len(calls)
    out = tmp_path / "svg-report"
    index = write_report([root], out)
    assert index["report_format_version"] == 7
    assert index["runs"][0]["requests"][0]["svg_view"]["status"] == status
    (item,) = index["runs"][0]["svg_gallery"]
    assert item["case_id"] == "svg-pelican-01"
    assert item["svg_view"]["status"] == status
    html = (out / "report.html").read_text()
    assert 'class="panel svg-gallery"' in html
    assert "SVG 生成展示 · 不评分" in html
    assert "style-src 'unsafe-inline'" in html
    assert "script-src 'unsafe-inline'" not in html
    script = re.findall(r"<script>(.*?)</script>", html, re.S)[-1]
    digest = base64.b64encode(hashlib.sha256(script.encode()).digest()).decode()
    assert f"script-src 'sha256-{digest}'" in html
    assert "&lt;svg" in html and "模型原始输出 / SVG 源码" in html
    if status == "ok":
        assert SVG in html
        assert html.count(SVG) == 2  # gallery showcase and case detail render the same bytes
    else:
        assert '<div class="svg-preview">' not in html
        assert "未能提取可渲染的 SVG" in html
    assert verify_report(out)["verified"]
    assert verify_artifact(out)["verified"]
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
    write_report([root], out)
    assert verify_report(out)["verified"]


def test_historical_v2_v3_and_v4_verify_and_non_svg_rows_are_identical(tmp_path, monkeypatch):
    # Digests from the complete frozen template sets, generated by their original implementations.
    assert template_hash(2) == "2ef4015ca3ebeecddec797cfea66a54472c22c264cbcdbdf21d604ea35fe2238"
    assert template_hash(3) == "658cd5f4f052a4b87da63c33e02378543ad92eb37f4a027e7a7901bb3fe4623c"
    # Stored historical scorer identity is evidence, not a demand to rescore during report rebuild.
    monkeypatch.setattr("inferyard.analysis.scoring.scorer_hash", lambda: "a" * 64)
    root = fixture_run(tmp_path / "runs")
    old = tmp_path / "v2"
    old.mkdir()
    index = build_index([root], old, format_version=2, producer="b" * 64)
    html = render_report_html(_environment(), template_name(2), index)
    (old / "index.json").write_bytes(json_bytes(index))
    (old / "report.html").write_text(html)
    assert verify_report(old)["verified"]
    assert verify_artifact(old)["verified"]
    assert "script-src 'unsafe-inline'" in html  # Historical bytes remain unchanged.
    frozen_v3 = tmp_path / "v3"
    frozen_v3.mkdir()
    index_v3 = build_index([root], frozen_v3, format_version=3, producer="b" * 64)
    html_v3 = render_report_html(_environment(), template_name(3), index_v3)
    (frozen_v3 / "index.json").write_bytes(json_bytes(index_v3))
    (frozen_v3 / "report.html").write_text(html_v3)
    monkeypatch.undo()
    assert verify_report(frozen_v3)["verified"]
    assert verify_artifact(frozen_v3)["verified"]
    assert "script-src 'sha256-" in html_v3
    assert index_v3["runs"] == index["runs"]  # v2/v3 share projection, no gallery key yet
    current = build_index([root], old, format_version=4, producer="b" * 64)
    assert current["runs"][0]["svg_gallery"] == []
    assert {k: v for k, v in current["runs"][0].items() if k != "svg_gallery"} == index["runs"][0]
    new = tmp_path / "v4"
    write_report([root], new)
    assert verify_report(new)["verified"]
    assert verify_artifact(new)["verified"]
