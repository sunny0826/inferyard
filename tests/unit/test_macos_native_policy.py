"""Synthetic IOKit policy and Mach swap observations, without device qualification."""

from types import SimpleNamespace

import pytest

from inferyard.platforms import macos_native, power_macos


@pytest.mark.parametrize("low", [True, False, None, -1, 2, 0.0, "1"])
def test_native_policy_rejects_invalid_low_power(low):
    assert power_macos.native_policy(True, low, None) is None


@pytest.mark.parametrize("mode", [True, False, -1, 3, 1.0, "1"])
def test_native_policy_rejects_invalid_power_mode(mode):
    assert power_macos.native_policy(True, 0, mode) is None


@pytest.mark.parametrize(
    "settings,expected",
    [
        ({"LowPowerMode": 0}, "unsupported"),
        ({"LowPowerMode": 1, "PowerMode": 2}, 2),
        ({"LowPowerMode": 0, "powermode": 0, "PowerMode": 2}, 0),
        ({"LowPowerMode": 0, "powermode": "bad", "PowerMode": 2}, None),
    ],
)
def test_active_policy_prefers_lowercase_and_only_absence_falls_back(
    monkeypatch, settings, expected
):
    core = SimpleNamespace(CFRelease=lambda value: None)
    iokit = SimpleNamespace(
        IOPSCopyPowerSourcesInfo=lambda: 1,
        IOPMCopyPMPreferences=lambda: 2,
        IOPSGetProvidingPowerSourceType=lambda blob: 3,
    )
    monkeypatch.setattr(power_macos, "_cf", lambda: (core, iokit))
    monkeypatch.setattr(power_macos, "_text", lambda *args: "AC Power")
    monkeypatch.setattr(power_macos, "_section", lambda *args: settings)

    def setting(core, section, name):
        value = section.get(name)
        if value is not None and type(value) is not int:
            raise ValueError("invalid_power_setting")
        return value

    monkeypatch.setattr(power_macos, "_setting", setting)
    ac, policy = power_macos.read_native()
    if expected is None:
        assert policy is None
    else:
        assert ac is True
        assert policy["power_mode"] == expected
        assert policy["source"] == "macos.iokit.power-policy.v1"
    monkeypatch.setattr(power_macos, "_text", lambda *args: "UPS Power")
    assert power_macos.read_native() == (None, None)


def test_swap_failure_never_reuses_previous_value_or_falls_back(monkeypatch):
    values = iter(((16384, 7, 8), (16384, None, 9), (None, None, None)))
    monkeypatch.setattr(macos_native, "_host_vm_swap", lambda: next(values))
    monkeypatch.setattr(macos_native, "query", lambda *args: pytest.fail("no subprocess fallback"))
    assert macos_native.swap_snapshot()["pswpin"] == 7
    second = macos_native.swap_snapshot()
    assert second["pswpin"] is None and second["pswpout"] == 9
    assert macos_native.swap_snapshot()["pswpout"] is None


def test_mach_swap_uses_vm_size_pointer_and_releases_port(monkeypatch):
    import ctypes

    calls = []

    class Call:
        def __init__(self, implementation):
            self.implementation = implementation

        def __call__(self, *args):
            return self.implementation(*args)

    def stats(port, flavor, result, count):
        assert port == 7 and flavor == 4
        result._obj.swapins, result._obj.swapouts = 11, 12
        return 0

    def page(port, value):
        assert type(value._obj) is ctypes.c_size_t
        value._obj.value = 16384
        return 0

    library = SimpleNamespace(
        mach_host_self=Call(lambda: 7),
        host_statistics64=Call(stats),
        host_page_size=Call(page),
    )
    monkeypatch.setattr(ctypes, "CDLL", lambda *args: library)
    monkeypatch.setattr(macos_native, "_release_host_port", lambda lib, port: calls.append(port))
    assert macos_native._host_vm_swap() == (16384, 11, 12)
    assert calls == [7]
    library.host_statistics64.implementation = lambda *args: 1
    assert macos_native._host_vm_swap() == (None, None, None)
    assert calls == [7, 7]
