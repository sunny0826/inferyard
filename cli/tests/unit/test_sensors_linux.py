from pathlib import Path

import pytest

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


def test_units_and_semantics_remain_separate(tmp_path):
    fixtures(tmp_path)
    collector = LinuxSensors(tmp_path)
    samples = collector.collect("formal", "req1")
    assert [s["value"] for s in samples] == [-1, 2000000000, 3000000000]
    assert [s["semantics"] for s in samples][1:] == [
        "hardware_reported",
        "requested_or_driver_reported",
    ]
    assert all(s["read_finished_ns"] >= s["read_started_ns"] for s in samples)
    assert collector.metadata()["missing_metrics"] == []


def test_identity_change_latched(tmp_path):
    zone, _ = fixtures(tmp_path)
    collector = LinuxSensors(tmp_path)
    (zone / "type").write_text("replacement")
    sample = collector.collect("formal", "req1")[0]
    assert sample["value"] is None
    assert sample["missing_reason"] == "source_changed"
    (zone / "type").write_text("x86_pkg_temp")
    assert collector.collect("formal", "req1")[0]["value"] is None


@pytest.mark.parametrize("raw", ["garbage", "-273151", "9" * 4097])
def test_invalid_temperature(tmp_path, raw):
    zone, _ = fixtures(tmp_path)
    collector = LinuxSensors(tmp_path)
    (zone / "temp").write_text(raw)
    sample = collector.collect("formal", "req1")[0]
    assert sample["value"] is None and sample["raw_value"] is None
    assert sample["missing_reason"].startswith("invalid_sensor")


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


def test_missing_and_limited_discovery(tmp_path):
    assert LinuxSensors(tmp_path).metadata()["missing_metrics"] == ["temperature", "frequency"]
    fixtures(tmp_path)
    collector = LinuxSensors(tmp_path, limit=1)
    assert len(collector.sources) == 1
    assert collector.metadata()["discovery_issues"]


def test_external_symlink_rejected(tmp_path):
    root = tmp_path / "sys"
    zone, _ = fixtures(root)
    external = tmp_path / "external"
    external.write_text("50000")
    (zone / "temp").unlink()
    from tests.helpers import symlink_or_skip

    symlink_or_skip(zone / "temp", external)
    sample = LinuxSensors(root).collect("formal", "req1")[0]
    assert sample["missing_reason"] == "source_outside_sysfs"
    assert sample["value"] is None


def test_schema_and_reductions_respect_sensor_and_window(tmp_path):
    from inferyard.analysis.observations import Observations
    from inferyard.analysis.sensor_observations import add_sensor_observations
    from inferyard.contracts.validation import ContractError, validate_document
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


def test_missing_and_cancelled_observations(tmp_path):
    from inferyard.analysis.observations import Observations
    from inferyard.analysis.sensor_observations import add_sensor_observations
    from tests.unit.test_observations import EVIDENCE, RUN
    from tests.unit.test_resources import ROW, envelope

    output = Observations(RUN, "w1", EVIDENCE, complete=True)
    add_sensor_observations(output, [ROW], [])
    assert len(output.items) == 2
    assert all(s["missing_reason"] == "sensor_evidence_unavailable" for s in output.items)
    fixtures(tmp_path)
    samples = [
        envelope(s) for s in LinuxSensors(tmp_path, clock=lambda: 10).collect("formal", "request-1")
    ]
    output = Observations(RUN, "w1", EVIDENCE, complete=True)
    add_sensor_observations(output, [{**ROW, "execution_state": "cancelled"}], samples)
    assert all(s["value"] is None for s in output.items)
    assert all(s["missing_reason"] == "request_not_validly_executed" for s in output.items)
