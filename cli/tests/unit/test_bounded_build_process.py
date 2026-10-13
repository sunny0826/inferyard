"""Real subprocess checks for the offline conversion supervisor."""

import importlib.util
import json
import os
import sys
from pathlib import Path

import pytest

pytestmark = pytest.mark.skipif(
    sys.platform != "linux", reason="Linux process-group build supervisor"
)

SPEC = importlib.util.spec_from_file_location(
    "bounded_build_process", Path(__file__).parents[2] / "scripts/bounded_build_process.py"
)
build = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(build)


def run(tmp_path, code, **kwargs):
    defaults = dict(
        environment=dict(os.environ),
        out=tmp_path / "evidence",
        wall_seconds=5,
        rss_bytes=1024**3,
        interval=0.02,
        check=lambda: {"temperature": 40},
    )
    defaults.update(kwargs)
    return build.run_build([sys.executable, "-c", code], **defaults)


def terminal(tmp_path):
    return json.loads((tmp_path / "evidence/terminal.json").read_text())


def test_success_and_output(tmp_path):
    result = run(tmp_path, "print('build-output')")
    assert result["state"] == "succeeded"
    assert result["exit_code"] == 0
    assert result["remaining_members"] == []
    assert "build-output" in (tmp_path / "evidence/process.log").read_text()


def test_nonzero_is_preserved_without_retry(tmp_path):
    result = run(tmp_path, "raise SystemExit(7)")
    assert result["state"] == "failed"
    assert result["exit_code"] == 7


def test_missing_safety_prevents_spawn(tmp_path):
    def missing():
        raise RuntimeError("sensor_unavailable")

    with pytest.raises(RuntimeError, match="sensor_unavailable"):
        run(tmp_path, "raise AssertionError('must not spawn')", check=missing)
    assert terminal(tmp_path)["owned_pid"] is None


def test_timeout_kills_term_ignoring_parent_and_child(tmp_path):
    code = (
        "import subprocess,sys,signal,time; "
        "signal.signal(signal.SIGTERM,signal.SIG_IGN); "
        "subprocess.Popen([sys.executable,'-c',"
        "'import signal,time;signal.signal(signal.SIGTERM,signal.SIG_IGN);time.sleep(60)']);"
        "time.sleep(60)"
    )
    with pytest.raises(TimeoutError):
        run(tmp_path, code, wall_seconds=0.4)
    result = terminal(tmp_path)
    assert result["state"] == "stopped"
    assert result["exit_code"] is not None
    assert result["remaining_members"] == []


def test_runtime_safety_failure_cleans_process(tmp_path):
    calls = 0

    def hot():
        nonlocal calls
        calls += 1
        if calls >= 3:
            raise RuntimeError("temperature_limit")
        return {"temperature": 40}

    with pytest.raises(RuntimeError, match="temperature_limit"):
        run(tmp_path, "import time;time.sleep(60)", check=hot)
    assert terminal(tmp_path)["remaining_members"] == []


def test_rss_stop(tmp_path):
    with pytest.raises(RuntimeError, match="rss_budget"):
        run(tmp_path, "import time;time.sleep(60)", rss_bytes=1)
    assert terminal(tmp_path)["remaining_members"] == []


def test_orphan_descendant_is_not_success(tmp_path):
    code = (
        "import subprocess,sys;subprocess.Popen([sys.executable,'-c','import time;time.sleep(60)'])"
    )
    with pytest.raises(RuntimeError, match="descendants_remain"):
        run(tmp_path, code)
    assert terminal(tmp_path)["remaining_members"] == []


def test_temperature_sensor_identity_and_disappearance(tmp_path):
    from inferyard.platforms.sensors_linux import LinuxSensors

    zone = tmp_path / "class/thermal/thermal_zone0"
    zone.mkdir(parents=True)
    (zone / "type").write_text("x86_pkg_temp")
    (zone / "temp").write_text("45000")
    check = build.BuildTemperatureCheck(90, sensors=LinuxSensors(tmp_path))
    assert check()["temperature_samples"][0]["value"] == 45
    (zone / "temp").write_text("90000")
    with pytest.raises(RuntimeError, match="threshold_reached"):
        check()
    (zone / "temp").write_text("45000")
    (zone / "type").write_text("changed_sensor")
    with pytest.raises(RuntimeError, match="source_unavailable"):
        check()
    (zone / "temp").unlink()
    with pytest.raises(RuntimeError, match="source_unavailable"):
        check()


def test_no_temperature_sources_rejected(tmp_path):
    from inferyard.platforms.sensors_linux import LinuxSensors

    with pytest.raises(RuntimeError, match="source_unavailable"):
        build.BuildTemperatureCheck(90, sensors=LinuxSensors(tmp_path))()


def test_hot_preflight_preserves_exact_sensor_evidence(tmp_path):
    from inferyard.platforms.sensors_linux import LinuxSensors

    zone = tmp_path / "class/thermal/thermal_zone0"
    zone.mkdir(parents=True)
    (zone / "type").write_text("x86_pkg_temp")
    (zone / "temp").write_text("91000")
    check = build.BuildTemperatureCheck(90, sensors=LinuxSensors(tmp_path))
    with pytest.raises(build.BuildSafetyStop):
        run(tmp_path, "raise AssertionError('must not start')", check=check)
    result = terminal(tmp_path)
    assert result["owned_pid"] is None
    evidence = result["stop_evidence"]
    assert evidence["threshold_celsius"] == 90
    assert evidence["temperature_samples"][0]["raw_value"] == 91000
    assert evidence["temperature_samples"][0]["source"] == "class/thermal/thermal_zone0/temp"


def test_pacing_records_stop_continue_and_finishes_cpu_work(tmp_path):
    result = run(
        tmp_path,
        "import time;start=time.process_time();\nwhile time.process_time()-start<0.15: pass",
        interval=0.2,
        pacing={"active_seconds": 0.02, "rest_seconds": 0.15},
    )
    assert result["state"] == "succeeded"
    events = [
        json.loads(line) for line in (tmp_path / "evidence/samples.jsonl").read_text().splitlines()
    ]
    controls = [e["control"] for e in events if "control" in e]
    assert controls and controls == ["SIGSTOP", "SIGCONT"] * (len(controls) // 2)
    assert result["elapsed_seconds"] > 0.3
    assert result["remaining_members"] == []


def test_wall_timeout_while_stopped_cleans_group(tmp_path):
    with pytest.raises(TimeoutError):
        run(
            tmp_path,
            "import time;time.sleep(60)",
            wall_seconds=0.1,
            interval=1,
            pacing={"active_seconds": 0.02, "rest_seconds": 0.8},
        )
    assert terminal(tmp_path)["remaining_members"] == []
    assert terminal(tmp_path)["exit_code"] is not None


@pytest.mark.parametrize(
    "pacing",
    [{}, {"active_seconds": True, "rest_seconds": 0.1}, {"active_seconds": 0.1, "rest_seconds": 2}],
)
def test_invalid_pacing_never_spawns(tmp_path, pacing):
    with pytest.raises(ValueError, match="pacing"):
        run(tmp_path, "raise AssertionError()", pacing=pacing)
    assert not (tmp_path / "evidence").exists()


def test_temperature_failure_while_paused_never_resumes(tmp_path):
    calls = 0

    def hot():
        nonlocal calls
        calls += 1
        if calls == 3:
            raise build.BuildSafetyStop("temperature_limit", {"value": 91})
        return {"value": 40}

    with pytest.raises(build.BuildSafetyStop):
        run(
            tmp_path,
            "import time;time.sleep(60)",
            interval=0.2,
            check=hot,
            pacing={"active_seconds": 0.02, "rest_seconds": 0.1},
        )
    events = [
        json.loads(line) for line in (tmp_path / "evidence/samples.jsonl").read_text().splitlines()
    ]
    assert [e["control"] for e in events if "control" in e] == ["SIGSTOP"]
    assert terminal(tmp_path)["remaining_members"] == []
    assert terminal(tmp_path)["stop_evidence"] == {"value": 91}
