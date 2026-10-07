"""Native macOS identity, TCP, resources and sealed workflows with a synthetic service."""

import asyncio
import json
import subprocess
import sys
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config.loader import load_config
from inferyard.config.planning import write_plan
from inferyard.config.single_plan import compile_single_plan
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import json_bytes, sha256_file, verify_manifest
from inferyard.platforms.identity import environment_snapshot, process_start_ticks
from inferyard.reporting.report import verify_report, write_report
from inferyard.runtime.batch_runner import execute_async as batch_execute
from inferyard.runtime.runner import execute_async
from scripts.create_macos_benchmark_config import render_toml
from tests.helpers import readline_timeout

pytestmark = pytest.mark.skipif(sys.platform != "darwin", reason="Native macOS runtime")


@pytest.fixture
def native_service(tmp_path, config_path, monkeypatch):
    import psutil

    monkeypatch.setattr("inferyard.runtime.lock.LEGACY_ROOT", None)
    monkeypatch.setattr("inferyard.runtime.lock.LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr("inferyard.runtime.lock.STATE_PATH", tmp_path / "host.state.json")
    model = tmp_path / "模拟 fixture.gguf"
    template = tmp_path / "template.jinja"
    model.write_bytes(b"synthetic file; not inference weights")
    template.write_text("fixture-template")
    script = Path(__file__).with_name("macos_synthetic_service.py").resolve()
    args = [
        str(script),
        "--model",
        str(model),
        "-ngl",
        "0",
        "-t",
        "2",
        "-tb",
        "2",
        "--reasoning",
        "off",
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
    ]
    process = subprocess.Popen(
        [sys.executable, *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
    )
    try:
        port = int(readline_timeout(process))
        engine = Path(psutil.Process(process.pid).exe()).resolve()
        libraries = tmp_path / "libraries.json"
        libraries.write_text(json.dumps({engine.name: sha256_file(engine)}))
        loaded = load_config(config_path)
        config = loaded.config.to_dict()
        config["endpoint"] = {
            "url": f"http://127.0.0.1:{port}",
            "server_pid": process.pid,
            "process_start_ticks": process_start_ticks(process.pid),
        }
        config["model"].update(
            local_path=str(model),
            sha256=sha256_file(model),
            template_path=str(template),
            template_sha256=sha256_file(template),
        )
        config["engine"].update(
            binary_path=str(engine),
            binary_sha256=sha256_file(engine),
            runtime_library_manifest=str(libraries),
            startup_args=args,
        )
        config["conditions"].update(
            profile="unknown",
            governor="unknown",
            epp="unknown",
            allow_unknown_environment=True,
            threads=2,
            threads_batch=2,
            model_loaded=True,
            cache_policy="disabled",
        )
        environment = environment_snapshot()
        if environment.get("profile") is not None:
            config["conditions"]["profile"] = environment["profile"]
        if environment.get("macos_power_policy") is not None:
            config["conditions"]["macos_power_policy"] = environment["macos_power_policy"]
        config["generation"]["seed_support"] = "supported"
        config["telemetry"].update(interval_ms=25, baseline_seconds=0.1, service_rss_required=True)
        config["execution"]["timeout_seconds"] = 5
        config["output"].update(root=str(tmp_path / "runs"), min_available_memory_bytes=1024**3)
        source = tmp_path / "config.toml"
        source.write_text(render_toml(config))
        yield load_config(source), process
    finally:
        if process.poll() is None:
            process.terminate()
        process.wait(timeout=5)
        process.stdout.close()
        process.stderr.close()


def assert_native_evidence(path, *, experiment):
    assert verify_manifest(path) == []
    data = read_trial(path)
    assert data["run"]["diagnostic"] is True
    assert data["summary"]["counts"]["completed"] == 2
    assert data["summary"]["counts"]["not_executed"] == 0
    identity = json.loads((path / "identity.json").read_text())
    assert identity["process"]["endpoint"] == "verified"
    assert identity["environment"]["platform"] == "Darwin"
    rss = [s for s in data["samples"] if s["metric_name"] == "service_rss"]
    assert rss and all(s["value"] > 0 for s in rss)
    assert {s["source"] for s in rss} == {"psutil:Process.memory_info:rss"}
    if experiment:
        collector = json.loads((path / "collector.json").read_text())
        assert collector["collector"] == "macos-resource.v1"
        cpu = [s for s in data["samples"] if s["metric_name"] == "service_cpu_ticks"]
        assert cpu and all(s["clock_ticks_per_second"] == 1_000_000 for s in cpu)
        assert any(s["value"] is not None for s in cpu)
    return data


def test_native_single_probe_run_and_offline_report(native_service, tmp_path):
    loaded, process = native_service
    request = CommandRequest("check", config=loaded, diagnostic=True)
    code, probe = asyncio.run(execute_async(request))
    assert code == 0, probe
    assert verify_manifest(Path(probe.evidence_dir)) == []
    code, result = asyncio.run(execute_async(replace(request, command="run")))
    assert code == 3 and "diagnostic_run_not_formal" in result.limitations, result
    path = Path(result.evidence_dir)
    data = assert_native_evidence(path, experiment=False)
    process.terminate()
    process.wait(timeout=5)
    report = tmp_path / "offline-report"
    write_report([path], report)
    assert (report / "report.html").is_file()
    assert verify_report(report)["verified"]
    assert data["summary"]["completeness"] == "incomplete"  # diagnostic stays diagnostic


def test_native_frozen_batch_uses_macos_collector_and_no_implicit_repeat(native_service, tmp_path):
    loaded, _ = native_service
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    config["bundle"]["path"] = str(tmp_path / "bundle.json")
    (tmp_path / "config.frozen.json").write_bytes(json_bytes(config))
    (tmp_path / "bundle.json").write_bytes(json_bytes(bundle))
    definition = compile_single_plan(config, bundle)["experiment"]
    definition["workloads"][0]["repeats"] = 2
    definition["budget"].update(max_requests=4, max_wall_seconds=300)
    source = tmp_path / "experiment.json"
    source.write_text(json.dumps(definition))
    frozen = tmp_path / "plan"
    write_plan(source, frozen)
    request = CommandRequest(
        "run", frozen_plan=frozen / "plan.json", output_root=tmp_path / "batch", diagnostic=True
    )
    code, result = asyncio.run(batch_execute(request))
    assert code == 0, result
    paths = [Path(p) for p in result.details["runs"]]
    assert len(paths) == 2
    for path in paths:
        assert_native_evidence(path, experiment=True)
    before = {path: sha256_file(path / "manifest.json") for path in paths}
    code, repeat = asyncio.run(batch_execute(request))
    assert code == 0 and repeat.details["runs"] == result.details["runs"]
    assert before == {path: sha256_file(path / "manifest.json") for path in paths}


def test_native_guardian_spawns_and_retains_required_temperature_policy(native_service, tmp_path):
    from inferyard.extensions.independent_guard import IndependentGuard

    loaded, service = native_service
    guard = IndependentGuard(tmp_path / "guard.jsonl", loaded.config.to_dict())
    try:
        guard.start()
        assert guard.process.pid != service.pid
    finally:
        guard.close()
    proof = guard.evidence()
    assert len(proof["samples"]) >= 2
    assert proof["policy"]["require_temperature"] is True
    first = proof["samples"][0]
    temperature = first["observation"]["temperature_samples"]
    if first["safe"]:
        assert temperature and all(row["value"] is not None for row in temperature)
        assert all(row["collector"] == "macos-smc.v1" for row in temperature)
    else:
        assert first["reason"]
    assert all(row["safe"] or row["reason"] for row in proof["samples"])
