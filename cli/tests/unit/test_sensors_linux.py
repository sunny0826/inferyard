from pathlib import Path

import pytest

from inferyard.errors import ContractError
from inferyard.platforms.sensors_linux import LinuxSensors


def fixtures(root):
    zone = root / "class/thermal/thermal_zone0"
    zone.mkdir(parents=True)
    (zone / "type").write_text("x86_pkg_temp")
    (zone / "temp").write_text("-1000")
    policy = root / "devices/system/cpu/cpufreq/policy0"
    policy.mkdir(parents=True)
    for name, value in {
        "scaling_driver": "test",
        "related_cpus": "0 1",
        "cpuinfo_cur_freq": "2000000",
        "scaling_cur_freq": "3000000",
    }.items():
        (policy / name).write_text(value)
    return zone, policy


def test_permission_failure_not_zero(tmp_path, monkeypatch):
    zone, _ = fixtures(tmp_path)
    collector = LinuxSensors(tmp_path)
    original = Path.open

    def denied(path, *args, **kwargs):
        if path == zone / "temp":
            raise PermissionError()
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "open", denied)
    sample = collector.collect("formal", "req1")[0]
    assert sample["missing_reason"] == "permission_denied"
    assert sample["value"] is None


def test_schema_and_reductions_respect_sensor_and_window(tmp_path):
    from inferyard.analysis.observations import Observations
    from inferyard.analysis.sensor_observations import add_sensor_observations
    from inferyard.contracts.validation import validate_document
    from tests.unit.test_observations import EVIDENCE, RUN
    from tests.unit.test_resources import ROW, envelope

    fixtures(tmp_path)
    collector = LinuxSensors(tmp_path, clock=lambda: 1_000_000_000)
    samples = [envelope(s, i + 1) for i, s in enumerate(collector.collect("formal", "request-1"))]
    for sample in samples:
        validate_document("sample", sample)
    with pytest.raises(ContractError, match="conversion"):
        validate_document("sample", {**samples[0], "value": 42})
    samples.append({**samples[0], "phase": "warmup", "value": 999})
    samples.append({**samples[0], "read_finished_ns": 3_000_000_000, "value": 999})
    output = Observations(RUN, "w1", EVIDENCE, complete=True)
    add_sensor_observations(output, [ROW], samples)
    assert len(output.items) == 5
    assert output.items[0]["value"] == -1
    assert output.items[0]["excluded"] == 2
    assert {s["value"] for s in output.items[1:]} == {2000000000, 3000000000}
    assert all(not s["comparison_eligible"] for s in output.items)
    assert all(s["coverage_ratio"] == 0 for s in output.items)
