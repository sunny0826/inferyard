"""Exercise production platform gates before any service request is possible."""

import asyncio
import os
from contextlib import nullcontext
from types import SimpleNamespace

import pytest

from inferyard.platforms.identity import PreflightError
from inferyard.runtime import batch_runner, trial_runner


class AfterPlatformGate(Exception):
    pass


def after_gate(*args, **kwargs):
    raise AfterPlatformGate


@pytest.mark.parametrize("name", ["nt", "posix"])
@pytest.mark.parametrize(
    "adapter", ["prism_llama_server_v1", "kvmem", "ninfer", "llama_cpp_b11146_v1"]
)
def test_batch_production_whitelist(tmp_path, monkeypatch, name, adapter):
    loaded = SimpleNamespace(
        config=SimpleNamespace(to_dict=lambda: {"engine": {"adapter": adapter}})
    )
    monkeypatch.setattr(batch_runner, "os", SimpleNamespace(name=name))
    monkeypatch.setattr(
        batch_runner, "TrialDependencies", lambda **kwargs: SimpleNamespace(lock=nullcontext)
    )
    monkeypatch.setattr(batch_runner, "open_batch", lambda *args, **kwargs: ({}, {"w1": loaded}))
    monkeypatch.setattr(batch_runner, "require_execution_support", lambda *args: None)
    monkeypatch.setattr(batch_runner, "history", after_gate)
    request = SimpleNamespace(
        command="run", output_root=tmp_path, diagnostic=False, frozen_plan=None
    )
    blocked = name == "nt" and adapter == "llama_cpp_b11146_v1"
    with pytest.raises(PreflightError if blocked else AfterPlatformGate) as caught:
        asyncio.run(batch_runner.execute_async(request))
    if blocked:
        assert str(caught.value) == "windows_phase2_live_not_supported"
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize("name", ["nt", "posix"])
@pytest.mark.parametrize(
    "adapter", ["prism_llama_server_v1", "kvmem", "ninfer", "llama_cpp_b11146_v1"]
)
def test_trial_production_whitelist(tmp_path, monkeypatch, name, adapter):
    loaded = SimpleNamespace(
        config=SimpleNamespace(to_dict=lambda: {"engine": {"adapter": adapter}}),
        bundle=SimpleNamespace(to_dict=lambda: {}),
    )
    monkeypatch.setattr(trial_runner, "os", SimpleNamespace(name=name, environ=os.environ))
    monkeypatch.setattr(trial_runner, "trial_for", after_gate)
    blocked = name == "nt" and adapter == "llama_cpp_b11146_v1"
    with pytest.raises(PreflightError if blocked else AfterPlatformGate) as caught:
        asyncio.run(trial_runner.run_trial({}, "t1", loaded, tmp_path))
    if blocked:
        assert str(caught.value) == "windows_phase2_live_not_supported"
    assert not list(tmp_path.iterdir())
