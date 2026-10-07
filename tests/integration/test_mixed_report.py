import asyncio
from pathlib import Path

import pytest

from inferyard.evidence.storage import json_bytes, sha256_file
from inferyard.reporting.report import verify_report, write_report
from inferyard.runtime.runner import execute_async
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario


def test_single_and_experiment_report_share_contract_preserve_denominators_and_bytes(
    scenario, tmp_path
):
    code, result = asyncio.run(execute_async(scenario[0], scenario[1]))
    assert code == 0
    legacy = Path(result.evidence_dir)
    plan, loaded, deps, output = inputs(scenario)
    code, _, modern = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    roots = [legacy, modern]
    before = {str(p): sha256_file(p) for root in roots for p in root.iterdir() if p.is_file()}
    calls = len(scenario[2])
    out = tmp_path / "mixed"
    index = write_report(roots, out)
    assert [r["schema_version"] for r in index["runs"]] == [3, 3]
    assert [r["summary"]["counts"]["planned"] for r in index["runs"]] == [3, 3]
    assert not any(index["comparison"]["eligibility"].values())
    assert index["comparison"]["completion_rate_difference"] is None
    assert index["runs"][0]["scan_view"]["length_bins"]
    assert "plan.json" in index["runs"][0]["evidence"]
    assert "metric_observations" in index["runs"][0]["summary"]
    html = (out / "report.html").read_text()
    assert "单次" in html and "实验" in html
    assert [r["execution_mode"] for r in index["runs"]] == ["single", "experiment"]
    assert verify_report(out)["source_runs"] == 2
    assert calls == len(scenario[2])
    assert before == {
        str(p): sha256_file(p) for root in roots for p in root.iterdir() if p.is_file()
    }


def test_unknown_source_version_is_not_guessed_as_legacy(tmp_path):
    source = tmp_path / "unknown"
    source.mkdir()
    (source / "run.json").write_bytes(json_bytes({"schema_version": 999}))
    from inferyard.evidence.formats import UnsupportedFormat

    with pytest.raises(UnsupportedFormat):
        write_report([source], tmp_path / "out")
    assert not (tmp_path / "out").exists()
