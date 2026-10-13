"""Run-start constants are copied; power, swap and free memory stay fresh."""

from inferyard.platforms.environment_constants import apply_constants, capture_constants
from inferyard.platforms.identity import environment_snapshot


def test_capture_keeps_only_constant_fields():
    captured = capture_constants(
        {"cpu_model": "test", "cpu_flags": None, "governor": "powersave", "extra": 1}
    )
    assert captured == {"cpu_model": "test", "cpu_flags": None}
    assert apply_constants({"cpu_model": "later", "governor": "ondemand"}, captured) == {
        "cpu_model": "test",
        "cpu_flags": None,
        "governor": "ondemand",
    }


def test_missing_start_fields_are_not_invented_and_values_are_detached():
    start = {"gpu": {"backend": "cpu"}, "cpu_model": None}
    captured = capture_constants(start)
    start["gpu"]["backend"] = "changed"
    current = apply_constants({"kernel": "later", "cpu_model": "later"}, captured)
    assert current == {"gpu": {"backend": "cpu"}, "cpu_model": None}
    current["gpu"]["backend"] = "changed again"
    assert captured["gpu"] == {"backend": "cpu"}
    assert apply_constants({"kernel": "later", "ac_online": True}, {}) == {"ac_online": True}


def test_periodic_copy_skips_cpuinfo_and_keeps_power_fresh(tmp_path, monkeypatch):
    proc = tmp_path / "proc"
    sys_root = tmp_path / "sys"
    proc.mkdir()
    (proc / "cpuinfo").write_text("model name : first\nflags : fpu\n")
    (proc / "meminfo").write_text("MemTotal: 2048 kB\nMemAvailable: 1024 kB\n")
    (proc / "vmstat").write_text("pswpin 1\npswpout 2\n")
    cpu = sys_root / "devices/system/cpu/cpu0/cpufreq"
    cpu.mkdir(parents=True)
    (cpu / "scaling_governor").write_text("powersave\n")
    (cpu / "scaling_driver").write_text("intel_pstate\n")
    (cpu / "energy_performance_preference").write_text("balance_performance\n")
    monkeypatch.setattr(
        "inferyard.platforms.identity.cpu_policy_snapshot",
        lambda _root: {"policies": [], "status": "unavailable"},
    )
    monkeypatch.setattr("inferyard.platforms.identity.memory_available", lambda _root: 1024 * 1024)
    monkeypatch.setattr("inferyard.platforms.identity.platform.system", lambda: "Linux")
    start = environment_snapshot(proc, sys_root)
    (proc / "cpuinfo").write_text("model name : later\nflags : changed\n")
    (cpu / "scaling_governor").write_text("performance\n")
    periodic = environment_snapshot(proc, sys_root, constants=capture_constants(start))
    assert periodic["cpu_model"] == "first"
    assert periodic["cpu_flags"] == ["fpu"]
    assert periodic["governor"] == "performance"
    assert periodic["swap_pages"] == {"pswpin": 1, "pswpout": 2}
    end = environment_snapshot(proc, sys_root)
    assert end["cpu_model"] == "later"
