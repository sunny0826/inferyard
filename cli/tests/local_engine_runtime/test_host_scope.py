"""Independent commands retain local process ownership and recovery barriers."""

import asyncio
from copy import deepcopy
from dataclasses import replace

import pytest

from inferyard.contracts.validation import Document
from inferyard.evidence.storage import read_json
from inferyard.platforms.identity import PreflightError
from inferyard.runtime import lock as locking
from inferyard.runtime.runner import execute_async
from tests.integration.test_local_engine_runtime import local_run  # noqa: F401
from tests.integration.test_runner import scenario  # noqa: F401
from tests.local_engine_transport.conftest import rig  # noqa: F401
from tests.unit.test_local_engine_contract import local_config  # noqa: F401


def test_independent_run_cannot_reuse_process_for_second_bundle(local_run):  # noqa: F811
    command, deps, state = local_run
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0, result.limitations
    assert len(state.posts) == 5
    bundle = command.config.bundle.to_dict()
    bundle["bundle_id"] = "second-scope"
    second = replace(
        command, config=replace(command.config, bundle=Document.parse("bundle", bundle))
    )
    code, result = asyncio.run(execute_async(second, deps))
    assert code == 2, result
    assert "local_scope_requires_new_service_process" in result.limitations
    assert len(state.posts) == 5


def test_dirty_recovery_rejects_surviving_worker(local_run, monkeypatch):  # noqa: F811
    command, deps, state = local_run
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0, result.limitations
    saved = read_json(locking.STATE_PATH)
    saved["dirty"] = True
    saved["local_service"]["service"]["workers"] = [{"pid": 99991, "start_ticks": 1234}]

    def ticks(pid):
        if pid == 99991:
            return 1234
        raise PreflightError("service_process_unavailable")

    monkeypatch.setattr(locking, "process_start_ticks", ticks)
    endpoint = deepcopy(command.config.config.to_dict()["endpoint"])
    endpoint.update(server_pid=99992, process_start_ticks=5678)
    with locking.HostLock() as lock:
        lock.write(saved)
        with pytest.raises(PreflightError, match="previous_local_process_still_alive"):
            lock.verify_manual_recovery(saved["dirty_token"], "replaced listener", endpoint)
    assert read_json(locking.STATE_PATH)["dirty"] is True
    assert len(state.posts) == 5
