"""Read-only native string boundaries; do not substitute cached or truncated data."""

import ctypes
from types import SimpleNamespace

import pytest

from inferyard.platforms import macos_identity, macos_native


@pytest.fixture
def native_string(monkeypatch):
    state = SimpleNamespace(payload=b"Apple M4\0", size=None, status=0, calls=[])

    class Call:
        def __call__(self, key, buffer, size, new_value, new_size):
            state.calls.append((key, len(buffer), new_value, new_size))
            ctypes.memmove(buffer, state.payload, min(len(buffer), len(state.payload)))
            size._obj.value = len(state.payload) if state.size is None else state.size
            return state.status

    state.call = Call()
    monkeypatch.setattr(
        ctypes, "CDLL", lambda *args, **kwargs: SimpleNamespace(sysctlbyname=state.call)
    )
    return state


def test_native_string_is_bounded_and_read_only(native_string):
    assert macos_native.sysctl_string("machdep.cpu.brand_string") == "Apple M4"
    assert native_string.calls == [(b"machdep.cpu.brand_string", 4096, None, 0)]
    assert native_string.call.restype is ctypes.c_int
    assert native_string.call.argtypes == [
        ctypes.c_char_p,
        ctypes.c_void_p,
        ctypes.POINTER(ctypes.c_size_t),
        ctypes.c_void_p,
        ctypes.c_size_t,
    ]


@pytest.mark.parametrize(
    "payload,size,status",
    [
        (b"Apple M4\0", None, -1),
        (b"Apple M4\0", 0, 0),
        (b"Apple M4\0", 4097, 0),
        (b"partial", None, -1),
        (b"Apple M4", None, 0),
        (b"Apple\0M4\0", None, 0),
        (b"Apple M4\0\0", None, 0),
        (b"\xff\0", None, 0),
        (b"\0", None, 0),
        (b"  \0", None, 0),
        (b"\x01\0\0\0", None, 0),
    ],
)
def test_native_string_failures_do_not_publish_partial_data(native_string, payload, size, status):
    native_string.payload, native_string.size, native_string.status = payload, size, status
    assert macos_native.sysctl_string("machdep.cpu.brand_string") is None


@pytest.mark.parametrize("key", [None, True, 123, b"name", "", "name\0other", "非ASCII"])
def test_invalid_string_key_never_calls_native_api(native_string, key):
    assert macos_native.sysctl_string(key) is None
    assert native_string.calls == []


@pytest.mark.parametrize("failure", ["missing_symbol", "load_error"])
def test_native_string_api_unavailable_is_missing(monkeypatch, failure):
    def unavailable(*args, **kwargs):
        if failure == "load_error":
            raise OSError("unavailable")
        return SimpleNamespace()

    monkeypatch.setattr(ctypes, "CDLL", unavailable)
    assert macos_native.sysctl_string("machdep.cpu.brand_string") is None


def test_native_string_reads_changes_and_failure_without_old_value(native_string):
    assert macos_native.sysctl_string("machdep.cpu.brand_string") == "Apple M4"
    native_string.payload = b"Apple changed\0"
    assert macos_native.sysctl_string("machdep.cpu.brand_string") == "Apple changed"
    native_string.status = -1
    assert macos_native.sysctl_string("machdep.cpu.brand_string") is None
    assert len(native_string.calls) == 3


def test_environment_uses_new_native_value_on_every_call(monkeypatch):
    values = iter(["Apple M4", "changed", None])
    monkeypatch.setattr(macos_identity, "sysctl_string", lambda key: next(values))
    monkeypatch.setattr(macos_identity, "query", lambda *args: None)
    monkeypatch.setattr(
        macos_identity,
        "swap_snapshot",
        lambda: {
            "pswpin": None,
            "pswpout": None,
            "page_size_bytes": None,
            "source": {
                "pswpin": "host_statistics64:swapins",
                "pswpout": "host_statistics64:swapouts",
            },
        },
    )
    monkeypatch.setattr(macos_identity, "boot_id", lambda: None)
    assert [macos_identity.environment_snapshot()["cpu_model"] for _ in range(3)] == [
        "Apple M4",
        "changed",
        None,
    ]
