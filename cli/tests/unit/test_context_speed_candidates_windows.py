"""Synthetic Windows assets retain receipt, device selection and binding checks."""

from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config import community_candidates, service_binding
from inferyard.config.loader import load_config
from inferyard.platforms import windows_runtime_prepare
from tests.unit.test_community_candidates import candidate_request
from tests.unit.test_community_runtime import synthetic_runtime as runtime_fixture
from tests.unit.test_device_preflight import gguf

synthetic_runtime = runtime_fixture


@pytest.mark.parametrize("context", [65536, 131072, 262144])
def test_windows_candidate_and_binding(synthetic_runtime, tmp_path, monkeypatch, context):
    runtime, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(runtime)
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    request = replace(
        candidate_request(tmp_path, model, receipt=Path(prepared["receipt"])),
        benchmark_profile="context-speed",
        context_size=context,
        bundle_path=Path(__file__).parents[2] / "bundles/zh-context-speed.json",
    )
    monkeypatch.setattr(community_candidates, "require_review", lambda _: None)
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    monkeypatch.setattr(
        community_candidates,
        "model_template",
        lambda _: (
            {
                "general.name": "chosen",
                "general.architecture": "fixture",
                "fixture.context_length": 262144,
            },
            b"fixture-template",
        ),
    )
    result = community_candidates.create(request)
    config = load_config(result["candidate"]).config.to_dict()
    args = config["engine"]["startup_args"]
    assert args[args.index("--device") + 1] == "CUDA0"
    assert args[args.index("--log-verbosity") + 1] == "4"
    assert config["conditions"]["context_size"] == context
    assert config["engine"]["runtime_library_manifest"] == prepared["runtime_library_manifest"]
    monkeypatch.setattr(service_binding, "process_start_ticks", lambda _: 456)
    monkeypatch.setattr(
        service_binding, "arguments", lambda _: [config["engine"]["binary_path"], *args]
    )
    bound = service_binding.prepare(
        CommandRequest(
            "config bind",
            candidate=Path(result["candidate"]),
            server_pid=123,
            endpoint_url=config["endpoint"]["url"],
            out=tmp_path / "bound",
        )
    )
    config["endpoint"].update(server_pid=123, process_start_ticks=456)
    assert load_config(bound["config"]).config.to_dict() == config
