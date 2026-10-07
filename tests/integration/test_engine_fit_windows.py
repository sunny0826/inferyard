"""Actual TCP service with synthetic Windows identity, resources and GPU readings."""

import shutil

import pytest

from inferyard.cli import main
from inferyard.config.engine_fit_native_sources import WINDOWS_SCOPE, WINDOWS_START_SOURCE
from inferyard.evidence.storage import read_json
from inferyard.reporting.engine_fit import verify
from tests.integration.test_engine_fit_common import common_fit_service as common_fit_service


@pytest.fixture
def windows_fit(common_fit_service, monkeypatch):
    from inferyard.platforms import engine_fit, engine_fit_windows_temperature

    fixture = common_fit_service
    runtime = fixture["runtime"]
    host = {"sha256": "a" * 64, "platform": "Windows", "architecture": "synthetic-amd64"}
    monkeypatch.setattr(runtime.platform, "system", lambda: "Windows")
    monkeypatch.setattr(runtime, "host_identity", lambda: host)
    monkeypatch.setattr(engine_fit, "host_identity", lambda: host)
    original_bind, original_snapshot = runtime.bind_service, runtime.resource_snapshot

    def bind(*args, **options):
        result = original_bind(*args, **options)
        result.update(
            start_ticks=134_090_000_000_000_123,
            process_start_source=WINDOWS_START_SOURCE,
            listener_source="GetExtendedTcpTable:owner_pid",
            listener_identity=result["listener_identity"].replace("macos:", "windows:", 1),
        )
        # The real Windows C volume reported this unsigned identity above 2**63.
        # Use it on every host so D-only test execution cannot hide the failure.
        result["model_binding"]["device"] = 10_090_975_144_720_226_164
        return result

    def snapshot(binding):
        result = original_snapshot(binding)
        result.update(scope=dict(WINDOWS_SCOPE), memory_available_bytes=0)
        return result

    monkeypatch.setattr(runtime, "bind_service", bind)
    monkeypatch.setattr(runtime, "resource_snapshot", snapshot)
    monkeypatch.setattr(engine_fit_windows_temperature.shutil, "which", lambda _: "synthetic-tool")
    monkeypatch.setattr(engine_fit_windows_temperature, "_read_query", lambda _: "0, 90\n")
    assert (
        main(
            [
                "engine-fit",
                "plan",
                "--model",
                str(fixture["model"]),
                "--engines",
                "llama-cpp",
                "--repetitions",
                "3",
                "--skip-temperature-stop",
                "synthetic Windows flow temperature override",
                "--skip-memory-stop",
                "synthetic Windows flow memory override",
                "--out",
                str(fixture["root"] / "windows-plan"),
            ]
        )
        == 0
    )
    return fixture


def run(fixture, name):
    return main(
        [
            "engine-fit",
            "run",
            "--plan",
            str(fixture["root"] / "windows-plan/plan.json"),
            "--engine",
            "llama-cpp",
            "--served-model",
            "synthetic",
            "--server-pid",
            "123",
            "--endpoint-url",
            fixture["origin"],
            "--out",
            str(fixture["root"] / name),
        ]
    )


def test_windows_tcp_completes_and_verifies_after_service_and_model_removed(windows_fit):
    fixture = windows_fit
    assert run(fixture, "windows-complete") == 0
    path = fixture["root"] / "windows-complete"
    data = verify(path)
    assert data["run"]["definition"] == "engine_fit_run.v6"
    assert data["run"]["platform"] == "Windows"
    assert data["run"]["counts"]["completed"] == data["plan"]["request_count"] == 9
    assert data["run"]["performance_comparison_qualified"] is False
    for resource in data["run"]["resources"]:
        assert resource["memory_available_bytes"] == 0
        assert resource["temperature_samples"][0]["value"] == 90
        assert resource["scope"] == WINDOWS_SCOPE
    fixture["close"]()
    shutil.rmtree(fixture["models_root"])
    assert main(["engine-fit", "verify", "--path", str(path)]) == 0
    assert read_json(fixture["root"] / "host.state.json")["dirty"] is False


def test_windows_tcp_disconnect_preserves_dirty_and_blocks_next_request(windows_fit):
    fixture = windows_fit
    fixture["state"]["broken"] = True
    assert run(fixture, "windows-broken") == 3
    data = verify(fixture["root"] / "windows-broken")
    assert data["run"]["counts"]["failed"] == 1
    assert data["run"]["counts"]["not_executed"] == 8
    assert read_json(fixture["root"] / "host.state.json")["dirty"] is True
    posts = len(fixture["state"]["posts"])
    assert run(fixture, "windows-dirty-rejected") == 2
    assert len(fixture["state"]["posts"]) == posts
    assert not (fixture["root"] / "windows-dirty-rejected").exists()


def test_windows_unsupported_engine_rejected_without_protocol_traffic(windows_fit):
    fixture = windows_fit
    assert (
        main(
            [
                "engine-fit",
                "run",
                "--plan",
                str(fixture["root"] / "windows-plan/plan.json"),
                "--engine",
                "lmstudio",
                "--served-model",
                "synthetic",
                "--server-pid",
                "123",
                "--endpoint-url",
                fixture["origin"],
                "--out",
                str(fixture["root"] / "unsupported"),
            ]
        )
        == 2
    )
    assert fixture["state"]["posts"] == [] and fixture["state"]["gets"] == []
