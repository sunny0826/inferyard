import asyncio
from pathlib import Path

import pytest

from inferyard.errors import UnsupportedFormat
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import sha256_file
from inferyard.reporting.studio_report import write_studio_report
from inferyard.application.execution import execute_async
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario


def test_single_and_experiment_studio_report_preserve_denominators_and_bytes(scenario, tmp_path):
    code, result = asyncio.run(execute_async(scenario[0], scenario[1]))
    assert code == 0
    single = Path(result.evidence_dir)
    plan, loaded, deps, output = inputs(scenario)
    code, _, experiment = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    roots = [single, experiment]
    before = {str(p): sha256_file(p) for root in roots for p in root.iterdir() if p.is_file()}
    calls = len(scenario[2])
    out = tmp_path / "mixed"
    document = write_studio_report(roots, out)
    runs = document["runs"]
    assert [r["source"]["path"] for r in runs] == [str(root.resolve()) for root in roots]
    assert [r["summary"]["completion_rate"]["denominator"] for r in runs] == [3, 3]
    assert [r["diagnostic"] for r in runs] == [False, True]
    assert "comparison" not in document and "scan_view" not in document
    assert len(runs[0]["requests"]) == runs[0]["summary"]["completion_rate"]["denominator"]
    assert {p.name for p in out.iterdir()} == {"studio.json"}
    assert calls == len(scenario[2])
    assert before == {
        str(p): sha256_file(p) for root in roots for p in root.iterdir() if p.is_file()
    }


def test_unknown_source_version_is_not_guessed_as_legacy(tmp_path):
    source = tmp_path / "unknown"
    source.mkdir()
    (source / "run.json").write_bytes(json_bytes({"schema_version": 999}))

    with pytest.raises(UnsupportedFormat):
        write_studio_report([source], tmp_path / "out")
    assert not (tmp_path / "out").exists()
