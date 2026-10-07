"""Fixed old HostLock must refuse before every protected live request path."""

import asyncio
from dataclasses import replace

import pytest

import inferyard.runtime.lock as locking
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.runner import execute_async
from tests.host_state_helpers import old_lock
from tests.integration.test_engine_fit import fit_service
from tests.integration.test_runner import scenario
from tests.integration.test_trial_runner import inputs

__all__ = ["scenario", "fit_service"]


def use_old(deps):
    deps.lock = lambda: old_lock(locking.STATE_PATH.parent)


@pytest.mark.parametrize("command", ["run", "check"])
def test_single_and_probe_retired_before_generation(scenario, command):
    request, deps, calls, _ = scenario
    use_old(deps)
    code, _ = asyncio.run(execute_async(replace(request, command=command), deps))
    assert code == 2 and calls == []


@pytest.mark.parametrize("command", ["run", "resume"])
def test_batch_and_resume_retired_before_generation(scenario, tmp_path, command):
    from inferyard.runtime.batch_runner import execute_async as batch_run
    from tests.integration.test_batch_runner import batch

    request, deps = batch(tmp_path, scenario)
    use_old(deps)
    request = replace(request, command=command, from_run=tmp_path / "batch/runs/absent")
    with pytest.raises(PreflightError, match="invalid_host_state"):
        asyncio.run(batch_run(request, deps))
    assert scenario[2] == []


def test_overhead_retired_before_generation(scenario):
    from inferyard.runtime.overhead_runner import run_overhead

    plan, loaded, deps, output = inputs(scenario)
    use_old(deps)
    with pytest.raises(PreflightError, match="invalid_host_state"):
        asyncio.run(
            run_overhead(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                output,
                tolerance_ratio=0.05,
                max_wall_seconds=60,
                dependencies=deps,
                common_observer=True,
            )
        )
    assert scenario[2] == []


def test_length_preparation_retired_before_management_or_generation(scenario, tmp_path):
    from inferyard.runtime.length_prepare import prepare
    from tests.unit.test_length_builder import spec

    request, deps, calls, _ = scenario
    use_old(deps)
    source = tmp_path / "length.json"
    source.write_bytes(json_bytes(spec()))
    request = replace(
        request, command="prepare-length", length_spec=source, out=tmp_path / "length"
    )
    with pytest.raises(PreflightError, match="invalid_host_state"):
        asyncio.run(prepare(request, deps))
    assert calls == [] and not request.out.exists()


def test_extension_retired_before_preflight_or_transport(scenario, tmp_path, monkeypatch):
    from inferyard.contracts.validation import Document
    from inferyard.extensions import workflow

    request, _, calls, _ = scenario
    config = request.config.config.to_dict()
    config["endpoint"]["api_key_env"] = "INFERYARD_TEST_EXTENSION_KEY"
    monkeypatch.setenv("INFERYARD_TEST_EXTENSION_KEY", "fixture-key")
    loaded = replace(request.config, config=Document.parse("config", config))
    monkeypatch.setattr(workflow, "HostLock", lambda: old_lock(locking.STATE_PATH.parent))

    def no_preflight(*args):
        pytest.fail("retired baseline reached live preflight")

    monkeypatch.setattr(workflow, "static_preflight", no_preflight)
    with pytest.raises(PreflightError, match="invalid_host_state"):
        asyncio.run(
            workflow.live(
                {"spec": {"definition": "closed_concurrency.v1"}}, loaded, tmp_path / "extension"
            )
        )
    assert calls == []


def test_engine_fit_retired_before_http_posts(fit_service, monkeypatch, capsys):
    from tests.integration.test_engine_fit import run_cli

    root, state, origin, runtime = fit_service
    monkeypatch.setattr(runtime, "HostLock", lambda: old_lock(locking.STATE_PATH.parent))
    assert run_cli(root, origin, "vllm", root / "retired-fit") == 2
    assert state["posts"] == []
    assert not (root / "retired-fit").exists()
    capsys.readouterr()


def test_complete_control_retired_before_transport(scenario, tmp_path, monkeypatch):
    from inferyard.extensions import complete_control_workflow as control

    request, _, calls, _ = scenario
    monkeypatch.setattr(control, "read_trial_plan", lambda *args: {})
    monkeypatch.setattr(control, "HostLock", lambda: old_lock(locking.STATE_PATH.parent))

    def no_preflight(*args):
        pytest.fail("retired baseline reached live preflight")

    monkeypatch.setattr(control, "static_preflight", no_preflight)
    with pytest.raises(PreflightError, match="invalid_host_state"):
        asyncio.run(control.live_complete({"spec": {}}, request.config, tmp_path / "control"))
    assert calls == []
