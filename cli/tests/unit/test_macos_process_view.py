"""Same-check views must preserve mapping proofs and detect subsequent changes."""

import json
from types import SimpleNamespace

import pytest

from inferyard.platforms import macos_identity, macos_process
from inferyard.platforms.identity import PreflightError, hash_file


class NativeError(Exception):
    pass


@pytest.fixture
def view(monkeypatch, tmp_path):
    engine_path = tmp_path / "server"
    model_path = tmp_path / "model.gguf"
    library = tmp_path / "libggml-metal.dylib"
    engine_path.write_bytes(b"synthetic executable")
    model_path.write_bytes(b"synthetic model")
    library.write_bytes(b"synthetic library")
    engine, model, metal = map(hash_file, (engine_path, model_path, library))
    manifest = tmp_path / "libraries.json"
    manifest.write_text(json.dumps({library.name: metal.sha256}))
    args = [engine.path, "--model", model.path, "-ngl", "1"]
    state = {"queries": [], "start": 1_700_000_000.125}
    state["rows"] = [
        {"p": 321, "f": "txt", "t": "REG", "D": hex(f.device), "i": str(f.inode)}
        for f in (engine, metal)
    ] + [
        {"p": 321, "f": "5", "t": "IPv4", "P": "TCP", "T": "ST=LISTEN", "n": "*:8080"},
        {"p": 321, "f": "cwd", "t": "DIR", "n": str(tmp_path)},
        {"p": 321, "f": "6", "t": "IPv4", "P": "TCP", "T": "ST=ESTABLISHED"},
    ]
    process = SimpleNamespace(
        _proc=SimpleNamespace(create_time=lambda *, monotonic: state["start"]),
        exe=lambda: engine.path,
        cmdline=lambda: args,
        cwd=lambda: str(tmp_path),
    )
    monkeypatch.setattr(
        macos_identity,
        "psutil_module",
        lambda: SimpleNamespace(
            Process=lambda pid: process, Error=NativeError, NoSuchProcess=NativeError
        ),
    )

    def read(pid, selectors):
        state["queries"].append((pid, list(selectors)))
        return [dict(row) for row in state["rows"]]

    monkeypatch.setattr(macos_identity, "lsof_records", read)
    config = {
        "endpoint": {"server_pid": 321, "process_start_ticks": 1_700_000_000_125000},
        "engine": {
            "startup_args": args[1:],
            "backend": "metal",
            "runtime_library_manifest": str(manifest),
        },
    }
    return state, process, config, model, engine, library, manifest


def verify(view):
    _, _, config, model, engine, *_ = view
    return macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)


def test_joint_metal_check_uses_one_fresh_pid_view(view):
    state, *_ = view
    observed = verify(view)
    assert state["queries"] == [(321, [])]
    assert observed["binary"] == observed["endpoint"] == "verified"
    assert observed["gpu_backend"]["source"] == "lsof:txt:file_identity"
    assert observed["gpu_backend"]["gpu_residency"] == "not_observed"
    assert "rows" not in observed and "queries" not in observed


@pytest.mark.parametrize(
    "index,changes,reason",
    [
        (0, {"f": "3"}, "binary_mapping_unverified"),
        (0, {"f": "cwd"}, "binary_mapping_unverified"),
        (0, {"p": 999}, "binary_mapping_unverified"),
        (0, {"i": "0"}, "binary_mapping_unverified"),
        (0, {"D": "0"}, "binary_mapping_unverified"),
        (0, {"t": "DIR"}, "binary_mapping_unverified"),
        (1, {"f": "3"}, "metal_backend_library_not_loaded"),
        (1, {"p": 999}, "metal_backend_library_not_loaded"),
        (1, {"i": "0"}, "metal_backend_library_not_loaded"),
        (1, {"D": "0"}, "metal_backend_library_not_loaded"),
        (2, {"p": 999}, "endpoint_pid_mismatch"),
        (2, {"T": "ST=ESTABLISHED"}, "endpoint_pid_mismatch"),
        (2, {"P": "UDP"}, "endpoint_pid_mismatch"),
        (2, {"t": "IPv6"}, "endpoint_pid_mismatch"),
        (2, {"n": "127.0.0.2:8080"}, "endpoint_pid_mismatch"),
        (2, {"n": "*:8081"}, "endpoint_pid_mismatch"),
    ],
)
def test_full_view_does_not_relax_component_proof(view, index, changes, reason):
    state, *_ = view
    state["rows"][index].update(changes)
    with pytest.raises(PreflightError, match=reason):
        verify(view)
    assert state["queries"] == [(321, [])]


@pytest.mark.parametrize("change", ["listener", "mapping", "duplicate", "empty"])
def test_next_check_reads_changed_rows_instead_of_reusing_previous_view(view, change):
    state, *_ = view
    verify(view)
    if change == "listener":
        state["rows"].pop(2)
        reason = "endpoint_pid_mismatch"
    elif change == "mapping":
        state["rows"].pop(1)
        reason = "metal_backend_library_not_loaded"
    elif change == "duplicate":
        state["rows"].append(dict(state["rows"][2]))
        reason = "endpoint_pid_mismatch"
    else:
        state["rows"] = []
        reason = "binary_mapping_unverified"
    with pytest.raises(PreflightError, match=reason):
        verify(view)
    assert state["queries"] == [(321, []), (321, [])]


@pytest.mark.parametrize("change", ["library", "manifest", "malformed_manifest"])
def test_next_check_rehashes_library_and_rereads_manifest(view, change):
    state, _, _, _, _, library, manifest = view
    verify(view)
    if change == "library":
        library.write_bytes(b"changed synthetic library")
    elif change == "manifest":
        manifest.write_text("{}")
    else:
        manifest.write_text('{"duplicate":1,"duplicate":2}')
    with pytest.raises(PreflightError):
        verify(view)
    assert len(state["queries"]) == 2


def test_pid_change_during_file_view_is_rejected(view, monkeypatch):
    state, *_ = view
    original = macos_identity.lsof_records

    def changed(pid, selectors):
        rows = original(pid, selectors)
        state["start"] += 1
        return rows

    monkeypatch.setattr(macos_identity, "lsof_records", changed)
    with pytest.raises(PreflightError, match="service_process_identity_changed"):
        verify(view)


def test_pid_change_at_final_check_is_rejected(view, monkeypatch):
    state, *_ = view
    original = macos_identity._metal_binding

    def changed(*args):
        result = original(*args)
        state["start"] += 1
        return result

    monkeypatch.setattr(macos_identity, "_metal_binding", changed)
    with pytest.raises(PreflightError, match="service_process_identity_changed"):
        verify(view)


@pytest.mark.parametrize("file", ["model", "engine"])
def test_file_change_after_mapping_still_fails_final_guard(view, monkeypatch, file):
    _, _, _, model, engine, *_ = view
    original = macos_identity._listener_identity

    def changed(*args):
        result = original(*args)
        from pathlib import Path

        Path((model if file == "model" else engine).path).write_bytes(b"changed file")
        return result

    monkeypatch.setattr(macos_identity, "_listener_identity", changed)
    with pytest.raises(PreflightError, match="identity_file_changed"):
        verify(view)


def test_explicit_empty_view_does_not_trigger_independent_query(view, monkeypatch):
    *_, engine, library, manifest = view

    def unexpected(*args):
        raise AssertionError("a supplied view must not trigger another query")

    monkeypatch.setattr(macos_process, "lsof_records", unexpected)
    assert not macos_process.mapped_file(321, engine, [])


def test_standalone_mapping_keeps_its_own_fresh_txt_query(view, monkeypatch):
    state, _, _, _, engine, *_ = view
    calls = []

    def read(pid, selectors):
        calls.append((pid, selectors))
        return [dict(state["rows"][0])]

    monkeypatch.setattr(macos_process, "lsof_records", read)
    assert macos_process.mapped_file(321, engine)
    state["rows"][0]["f"] = "3"
    assert not macos_process.mapped_file(321, engine)
    assert calls == [(321, ["-d", "txt"]), (321, ["-d", "txt"])]
