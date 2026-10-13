"""Operator wrapper lifecycle checks without a model workload."""

import importlib.util
import json
import subprocess
import sys
from pathlib import Path

import pytest

PATH = Path(__file__).resolve().parents[2] / "scripts/operator_acceptance.py"
SPEC = importlib.util.spec_from_file_location("operator_acceptance", PATH)
wrapper = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(wrapper)


@pytest.mark.parametrize("mismatch", [None, "source", "preview", "frozen"])
def test_verify_binds_explicit_packet_to_expected_hashes(tmp_path, monkeypatch, mismatch):
    packet = tmp_path / "packet"
    (packet / "input").mkdir(parents=True)
    (packet / "frozen").mkdir()
    out = tmp_path / "output"
    out.mkdir()
    fixture = Path(__file__).parents[1] / "fixtures/operator/safety.json"
    wrapper.save(packet / "input/experiment.json", {})
    wrapper.save(
        packet / "frozen/plan.json",
        {
            "plan_sha256": "wrong" if mismatch == "frozen" else "expected-plan",
            "experiment": {"safety": json.loads(fixture.read_text(encoding="utf-8"))},
            "runtime_bindings": [{"config": {"path": "config.json"}}],
        },
    )
    wrapper.save(packet / "frozen/config.json", {"conditions": {"require_epp_match": False}})
    (packet / "service-command.txt").write_text("fixture-server --port 48857", encoding="utf-8")

    def command(args, directory, name):
        assert directory == out
        if name == "source":
            (out / "source.stdout").write_text(
                "wrong" if mismatch == "source" else "expected-source", encoding="utf-8"
            )
        else:
            assert args[-2] == str(packet / "input/experiment.json")
            wrapper.save(
                out / "preview.stdout",
                {
                    "details": {
                        "plan_sha256": "wrong" if mismatch == "preview" else "expected-plan",
                        "request_limit": 40,
                        "trials": [{}],
                    }
                },
            )
        return 0

    monkeypatch.setattr(wrapper, "command", command)
    if mismatch is not None:
        with pytest.raises(RuntimeError, match="源码|计划"):
            wrapper.verify(out, packet, "expected-source", "expected-plan")
    else:
        assert wrapper.verify(out, packet, "expected-source", "expected-plan") == [
            "fixture-server",
            "--port",
            "48857",
        ]


def test_command_preserves_nonzero_exit(tmp_path):
    code = wrapper.command([sys.executable, "-c", "raise SystemExit(7)"], tmp_path, "step")
    assert code == 7
    assert json.loads((tmp_path / "step.exit.json").read_text())["exit_code"] == 7


@pytest.mark.skipif(sys.platform != "linux", reason="Linux historical operator process groups")
def test_timeout_reaps_owned_child(tmp_path, monkeypatch):
    children = []
    popen = subprocess.Popen

    def launch(*args, **kwargs):
        child = popen(*args, **kwargs)
        children.append(child)
        return child

    monkeypatch.setattr(wrapper.subprocess, "Popen", launch)
    with pytest.raises(TimeoutError):
        wrapper.command(
            [sys.executable, "-c", "import time; time.sleep(60)"], tmp_path, "step", timeout=0.02
        )
    assert len(children) == 1 and children[0].poll() is not None
    assert json.loads((tmp_path / "step.exit.json").read_text())["reason"] == "wrapper_timeout"


@pytest.mark.skipif(sys.platform != "linux", reason="Linux historical operator process groups")
def test_service_readiness_failure_stops_child(tmp_path, monkeypatch):
    monkeypatch.setattr(wrapper, "record_start_temperature", lambda _: None)

    class FreePort:
        def __enter__(self):
            return self

        def __exit__(self, *args):
            pass

        def bind(self, address):
            pass

    monkeypatch.setattr(wrapper.socket, "socket", FreePort)
    children = []

    def fail_ready(process):
        children.append(process)
        raise TimeoutError("injected readiness timeout")

    monkeypatch.setattr(wrapper, "ready", fail_ready)
    with pytest.raises(TimeoutError):
        wrapper.run_service(
            [sys.executable, "-c", "import time; time.sleep(60)"], tmp_path, tmp_path
        )
    assert children[0].poll() is not None
    assert (tmp_path / "service-stop.json").exists()


def test_stopped_run_can_export_without_human_signoff(tmp_path, monkeypatch):
    run = tmp_path / "batch/runs/example"
    wrapper.save(
        tmp_path / "run.stdout",
        {
            "run_id": "example",
            "status": "stopped",
            "completeness": "incomplete",
            "details": {
                "runs": [str(run)],
                "last_stop_reason": "temperature_safety_threshold_reached",
            },
        },
    )
    monkeypatch.setattr(wrapper, "command", lambda *a, **kw: 0)
    signoff = {
        "operator": "tester",
        "checks": {"forty_case_ledger": None},
        "status": "pending_independent_operator",
    }
    assert wrapper.offline(tmp_path, signoff)
    result = json.loads((tmp_path / "automatic-checks.json").read_text())
    assert result["completeness"] == "incomplete"
    assert result["independent_operator_requirement"] == "cancelled_by_user"
    saved = json.loads((tmp_path / "signoff.json").read_text())
    assert saved["checks"]["forty_case_ledger"] is None
    assert saved["status"] == "pending_independent_operator"


@pytest.mark.parametrize("value", [99.0, None])
def test_no_temperature_gate_but_observations_preserved(value, tmp_path, monkeypatch):
    from inferyard.platforms import sensors_linux
    from inferyard.runtime.safety import SafetyGuard

    class Sensors:
        sources = [{"metric_name": "temperature"}]

        def collect(self, phase, request_id):
            return [
                {"value": value, "missing_reason": "source_unavailable" if value is None else None}
            ]

    monkeypatch.setattr(sensors_linux, "LinuxSensors", Sensors)
    wrapper.record_start_temperature(tmp_path)
    snapshot = json.loads((tmp_path / "start-temperature.json").read_text())
    assert snapshot["used_as_gate"] is False
    assert snapshot["samples"][0]["value"] == value
    fixture = Path(__file__).parents[1] / "fixtures/operator/safety.json"
    safety = json.loads(fixture.read_text(encoding="utf-8"))
    policy = dict(safety, check_environment=False, max_external_cpu_percent=None)
    guard = SafetyGuard(policy, {}, lambda: {}, sensors=Sensors())
    guard.check(periodic=True)
    assert guard.last["temperature_samples"][0]["value"] == value
    assert safety["check_environment"] is True
    assert safety["max_external_cpu_percent"] == 25
