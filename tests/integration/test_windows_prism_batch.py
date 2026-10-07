"""Mock HTTP batch orchestration with the production Windows collector selection."""

import asyncio
import os
from pathlib import Path
from types import SimpleNamespace

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import read_json, sha256_file
from inferyard.platforms import resources, resources_windows
from inferyard.registry import collector_factory
from inferyard.runtime import batch_runner, trial_runner
from tests.integration.test_batch_runner import batch
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.test_windows_resources import AccessDenied, NoSuchProcess, PsutilError

scenario = runner_scenario


def test_windows_prism_batch_reaches_collector_and_seals_mock_requests(
    tmp_path, scenario, monkeypatch
):
    request, deps = batch(tmp_path, scenario)
    ticks = scenario[0].config.config.to_dict()["endpoint"]["process_start_ticks"]
    monkeypatch.setattr(resources_windows, "process_start_ticks", lambda pid: ticks)
    fake_psutil = SimpleNamespace(
        Error=PsutilError,
        AccessDenied=AccessDenied,
        NoSuchProcess=NoSuchProcess,
        Process=lambda pid: SimpleNamespace(memory_info=lambda: SimpleNamespace(rss=8192)),
        virtual_memory=lambda: SimpleNamespace(available=16 * 1024**3),
    )
    monkeypatch.setattr(resources_windows, "psutil_module", lambda: fake_psutil)
    monkeypatch.setattr(batch_runner, "os", SimpleNamespace(name="nt"))
    monkeypatch.setattr(trial_runner, "os", SimpleNamespace(name="nt", environ=os.environ))
    monkeypatch.setattr(resources, "sys", SimpleNamespace(platform="win32"))
    monkeypatch.setattr(batch_runner, "TrialDependencies", lambda **kwargs: deps)
    monkeypatch.setattr(batch_runner, "adapter_factory", lambda identifier: deps.adapter)
    # No dependencies argument: execute the actual Windows gate and collector factory.
    code, result = asyncio.run(batch_runner.execute_async(request))
    assert code == 0 and result.status == "finished"
    assert len(scenario[2]) == 24  # Three repetitions: probe/warmup/formal fixture protocol.
    assert deps.sampler is collector_factory("windows-resource.v1")
    for path in result.details["runs"]:
        data = read_trial(Path(path))
        assert data["summary"]["counts"]["completed"] == 3
        root = Path(path)
        assert read_json(root / "collector.json")["collector"] == "windows-resource.v1"
        manifest = read_json(root / "manifest.json")
        assert manifest["files"]["collector.json"]["sha256"] == sha256_file(root / "collector.json")
        assert data["samples"]
        assert all(s["unit"] == "bytes" for s in data["samples"])
        assert {s["source"] for s in data["samples"]} == {
            "psutil:virtual_memory:available",
            "psutil:Process.memory_info:rss",
        }
        missing = [
            m
            for m in data["summary"]["metric_observations"]
            if m["metric_id"] in ("C04", "C05", "C06", "C07", "C08")
        ]
        assert missing and all(m["value"] is None and m["missing_reason"] for m in missing)
        assert not data["summary"]["measurement_context"]["performance_comparison_eligible"]
