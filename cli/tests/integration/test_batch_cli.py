"""Batch CLI keeps machine-readable output and rejects invalid command combinations."""

import json
import sys

import pytest

from inferyard.cli import main
from tests.integration.test_batch_runner import batch, scenario


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
    assert output.err


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
