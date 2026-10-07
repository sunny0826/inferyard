"""Native preparation logic under injected observations; no live engine or service."""

import json
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config import community_candidates, preparation_io, service_binding
from inferyard.config.loader import load_config
from inferyard.config.preparation_io import PreparationError
from inferyard.platforms import windows_runtime_prepare
from tests.unit.test_community_runtime import synthetic_runtime as runtime_fixture
from tests.unit.test_device_preflight import gguf

synthetic_runtime = runtime_fixture
ROOT = Path(__file__).parents[2]


def candidate_request(tmp_path, model, *, receipt=None, engine=None, system="Windows"):
    preflight = tmp_path / "preflight.json"
    selected = dict(
        model_path=str(model),
        mode="cuda" if system == "Windows" else "metal",
        threads=2,
        context_size=4096,
        min_available_memory_bytes=1073741824,
        min_disk_bytes=5368709120,
    )
    if system == "Windows":
        selected["gpu_index"] = 0
    preflight.write_text(
        json.dumps(
            {
                "hardware": {"platform": system},
                "recommendation": {"status": "recommended", "selected": selected},
            }
        )
    )
    return CommandRequest(
        "config create",
        preflight=preflight,
        bundle_path=ROOT / "bundles/zh-smoke.json",
        output_root=tmp_path / "results",
        out=tmp_path / "candidate",
        runtime_receipt=receipt,
        engine_path=engine,
        model_repo="local",
        port=48857,
    )


def test_windows_candidate_references_verified_runtime_and_binding_keeps_recipe(
    synthetic_runtime, tmp_path, monkeypatch
):
    runtime, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(runtime)
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    request = candidate_request(tmp_path, model, receipt=Path(prepared["receipt"]))
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    result = community_candidates.create(request)
    config = load_config(result["candidate"]).config.to_dict()
    assert config["engine"]["runtime_library_manifest"] == prepared["runtime_library_manifest"]
    assert not (request.out / "engine-sha256.json").exists()
    assert not any(p.suffix in (".exe", ".dll") for p in request.out.iterdir())
    assert config["output"]["root"] == str(request.output_root)
    assert config["generation"]["temperature"] == 0
    monkeypatch.setattr(service_binding, "process_start_ticks", lambda pid: 456)
    monkeypatch.setattr(
        service_binding,
        "arguments",
        lambda pid: [config["engine"]["binary_path"], *config["engine"]["startup_args"]],
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


def test_macos_candidate_preserves_existing_generation_and_exact_template(tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    engine = tmp_path / "llama-server"
    engine.write_bytes(b"synthetic, never executed")
    (tmp_path / "libggml-metal.dylib").write_bytes(b"library")
    request = candidate_request(tmp_path, model, engine=engine, system="Darwin")
    monkeypatch.setattr(community_candidates, "fresh_macos", lambda selected, _: selected)
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    monkeypatch.setattr(
        community_candidates, "model_template", lambda _: ({"general.name": "chosen"}, b"x\r\ny\r")
    )
    result = community_candidates.create(request)
    config = load_config(result["candidate"]).config.to_dict()
    assert Path(result["template"]).read_bytes() == b"x\r\ny\r"
    assert config["generation"]["temperature"] == 0.7
    assert config["engine"]["backend"] == "metal"


@pytest.mark.parametrize(
    "change",
    ["bool_threads", "wrong_platform", "blocked", "wrong_mode", "bad_gpu", "results_inside"],
)
def test_windows_candidate_rejects_bad_recommendation_before_engine_execution(
    synthetic_runtime, tmp_path, monkeypatch, change
):
    runtime, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(runtime)
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    request = candidate_request(tmp_path, model, receipt=Path(prepared["receipt"]))
    report = json.loads(request.preflight.read_text())
    selected = report["recommendation"]["selected"]
    if change == "bool_threads":
        selected["threads"] = True
    elif change == "wrong_platform":
        report["hardware"]["platform"] = "Linux"
    elif change == "blocked":
        report["recommendation"]["status"] = "blocked"
    elif change == "wrong_mode":
        selected["mode"] = "cpu"
    elif change == "bad_gpu":
        selected["gpu_index"] = False
    else:
        request = replace(request, output_root=request.out / "results")
    request.preflight.write_text(json.dumps(report))
    monkeypatch.setattr(
        community_candidates, "query_version", lambda *a, **k: pytest.fail("must not execute")
    )
    with pytest.raises(PreparationError):
        community_candidates.create(request)
    assert not request.out.exists()


@pytest.mark.parametrize("failure", ["ticks", "argv"])
def test_binding_mismatch_does_not_write(tmp_path, config_path, monkeypatch, failure):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Linux", "x64"))
    config = load_config(config_path).config.to_dict()
    ticks = iter([1, 2] if failure == "ticks" else [1, 1])
    monkeypatch.setattr(service_binding, "process_start_ticks", lambda pid: next(ticks))
    argv = [
        "engine",
        *config["engine"]["startup_args"],
        *(["--changed"] if failure == "argv" else []),
    ]
    monkeypatch.setattr(service_binding, "arguments", lambda pid: argv)
    with pytest.raises(PreparationError):
        service_binding.prepare(
            CommandRequest(
                "config bind",
                candidate=config_path,
                server_pid=123,
                endpoint_url=config["endpoint"]["url"],
                out=tmp_path / "new",
            )
        )
    assert not (tmp_path / "new").exists()
