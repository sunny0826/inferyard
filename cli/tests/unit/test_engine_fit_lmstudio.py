"""LM Studio native declaration and observer refusals, without running a model."""

import asyncio
import hashlib
import os
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.platforms import engine_fit as fit
from inferyard.platforms import engine_fit_lmstudio as lms
from inferyard.platforms import engine_fit_lmstudio_files as files
from inferyard.platforms import engine_fit_macos as macos
from inferyard.platforms.identity import PreflightError
from tests.unit import test_engine_fit_macos_identity as mac_fixtures

native = mac_fixtures.native


@pytest.fixture
def studio(native, monkeypatch):
    state, _, _, root = native
    model = root / "model.gguf"
    model.write_bytes(b"synthetic GGUF")
    state["rows"].append(
        {
            "p": 321,
            "f": "txt",
            "t": "REG",
            "D": hex(model.stat().st_dev),
            "i": str(model.stat().st_ino),
        }
    )
    native[1].ppid = lambda: 1
    native[1].children = lambda *, recursive: []
    native[1].status = lambda: "running"
    monkeypatch.setattr(files, "lsof_records", lambda pid, selectors, **kw: deepcopy(state["rows"]))
    cli = root.parent / "lms"
    cli.write_bytes(b"synthetic lms CLI")
    cli.chmod(0o700)
    old = Path(state["exe"])
    renamed = old.with_name("llmster")
    old.rename(renamed)
    state.update(exe=str(renamed), args=[str(renamed)])
    state["loaded"] = [
        {
            "type": "llm",
            "format": "gguf",
            "identifier": "test-model",
            "path": "model.gguf",
            "deviceIdentifier": None,
            "status": "idle",
            "queued": 0,
            "private_extra": "not-for-evidence",
        }
    ]
    state["link"] = {"issues": ["deviceDisabled"], "deviceName": "private-host"}
    state["commands"] = []

    def query(command, *, deadline=None):
        state["commands"].append(command)
        assert command[0] == str(cli.resolve())
        assert command[-3:] == ["--json", "--port", "8080"]
        assert "--host" not in command
        return deepcopy(state["link"] if command[1:3] == ["link", "status"] else state["loaded"])

    monkeypatch.setattr(lms, "_run", query)
    monkeypatch.setattr(fit.platform, "system", lambda: "Darwin")
    return state, model, cli, root


def bind(studio):
    _, model, cli, root = studio
    return fit.bind_service(
        "lmstudio",
        model,
        321,
        "http://127.0.0.1:8080",
        lms_path=cli,
        models_root=root,
        served_model="test-model",
    )


def test_native_lms_binding_and_observer_never_discover_or_persist_private_output(studio):
    state, model, cli, root = studio
    binding = bind(studio)
    assert binding["model_binding"]["source"] == "lms_loaded_instance_path"
    assert binding["model_binding"]["path"] == str(model.resolve())
    assert binding["observer"] == {
        "kind": "lms",
        "path": str(cli.resolve()),
        "models_root": str(root.resolve()),
        "instance_id": "test-model",
        "sha256": hashlib.sha256(cli.read_bytes()).hexdigest(),
    }
    observer = lms.LMStudioObserver(binding)
    service = asyncio.run(observer.inspect())
    assert service["version"] is None
    assert service["version_source"] == "not_exposed"
    assert service["version_missing_reason"] == "lmstudio_service_version_not_exposed"
    idle = asyncio.run(observer.idle())
    assert idle == {
        "idle": True,
        "source": "lms:ps",
        "values": [
            {"metric": "lmstudio:queued", "labels": {"instance": "test-model"}, "value": 0},
            {"metric": "lmstudio:active", "labels": {"instance": "test-model"}, "value": 0},
        ],
    }
    assert "private-host" not in repr((binding, service, idle))
    assert "not-for-evidence" not in repr((binding, service, idle))
    fit.check_service(binding)
    assert all(command[1] in ("ps", "link") for command in state["commands"])


@pytest.mark.parametrize(
    "address,port",
    [
        ("192.0.2.1", 8080),
        ("localhost", 8080),
        ("127.0.0.2", 8080),
        ("::1", 8080),
        (None, 8080),
        ("", 8080),
        ("127.0.0.1", None),
        ("127.0.0.1", True),
        ("127.0.0.1", False),
        ("127.0.0.1", 0),
        ("127.0.0.1", -1),
        ("127.0.0.1", 65536),
        ("127.0.0.1", "8080"),
        ("127.0.0.1", 8080.0),
    ],
)
def test_unverified_endpoint_is_rejected_before_any_cli_call(studio, address, port):
    state, model, cli, root = studio
    with pytest.raises(PreflightError, match="lms_endpoint_unverified"):
        lms.bind_observer(cli, root, "test-model", model, address, port)
    assert state["commands"] == []


@pytest.mark.parametrize("port", [1, 65535])
def test_cli_uses_exact_valid_port_with_local_authentication(studio, monkeypatch, port):
    state, model, cli, root = studio

    def query(command, *, deadline):
        state["commands"].append(command)
        assert command in (
            [str(cli.resolve()), "link", "status", "--json", "--port", str(port)],
            [str(cli.resolve()), "ps", "--json", "--port", str(port)],
        )
        return deepcopy(state["link"] if command[1] == "link" else state["loaded"])

    monkeypatch.setattr(lms, "_run", query)
    lms.bind_observer(cli, root, "test-model", model, "127.0.0.1", port)
    assert len(state["commands"]) == 2


@pytest.mark.parametrize(
    "status,queued",
    [
        ("idle", 1),
        ("processingPrompt", 1),
        ("generating", 1),
        ("computingEmbedding", 0),
    ],
)
def test_idle_requires_both_idle_state_and_zero_inclusive_queue(studio, status, queued):
    observer = lms.LMStudioObserver(bind(studio))
    studio[0]["loaded"][0].update(status=status, queued=queued)
    result = asyncio.run(observer.idle())
    assert result["idle"] is False
    assert result["values"][0]["value"] == queued
    assert result["values"][1]["value"] == int(status != "idle")


@pytest.mark.parametrize(
    "key,value",
    [
        ("identifier", "other"),
        ("type", "embedding"),
        ("format", "mlx"),
        ("deviceIdentifier", "remote-device"),
        ("status", "ready"),
        ("status", []),
        ("queued", True),
        ("queued", -1),
        ("queued", 0.5),
        ("queued", None),
        ("path", "../model.gguf"),
        ("path", "/model.gguf"),
        ("path", "a/../model.gguf"),
        ("path", "./model.gguf"),
        ("path", "a\\model.gguf"),
        ("path", "model.gguf\0"),
        ("path", ""),
        ("path", "missing.gguf"),
    ],
)
def test_unverified_instance_path_or_state_is_rejected_before_generation(studio, key, value):
    studio[0]["loaded"][0][key] = value
    with pytest.raises(PreflightError):
        bind(studio)


@pytest.mark.parametrize("key", ["deviceIdentifier", "queued", "status", "path"])
def test_absent_fields_are_not_inferred_from_loaded_models(studio, key):
    del studio[0]["loaded"][0][key]
    with pytest.raises(PreflightError):
        bind(studio)


@pytest.mark.parametrize("loaded", [[], [{}, {}], {}, None, ["model"]])
def test_missing_or_multiple_loaded_models_cannot_prove_idle(studio, loaded):
    studio[0]["loaded"] = loaded
    with pytest.raises(PreflightError, match="loaded_instance_ambiguous"):
        bind(studio)


@pytest.mark.parametrize(
    "link",
    [
        {},
        {"issues": []},
        {"issues": ["notLoggedIn"]},
        {"issues": "deviceDisabled"},
        {"issues": ["deviceDisabled", None]},
        {"status": "offline", "issues": []},
        None,
    ],
)
def test_link_offline_or_unknown_is_not_explicitly_disabled(studio, link):
    studio[0]["link"] = link
    with pytest.raises(PreflightError, match="link_not_disabled"):
        bind(studio)


def test_model_symlink_cannot_escape_declared_root(studio):
    state, model, _, root = studio
    outside = root.parent / "outside.gguf"
    model.rename(outside)
    model.symlink_to(outside)
    state["loaded"][0]["path"] = model.name
    with pytest.raises(PreflightError, match="model_path_unverified"):
        bind(studio)


@pytest.mark.parametrize("change", ["cli", "model", "instance", "link", "service"])
def test_bound_evidence_changes_are_rejected(studio, change):
    state, model, cli, _ = studio
    binding = bind(studio)
    if change == "cli":
        cli.write_bytes(b"changed CLI")
    elif change == "model":
        model.rename(model.with_suffix(".old"))
        model.write_bytes(b"new file")
    elif change == "instance":
        state["loaded"][0]["identifier"] = "other"
    elif change == "link":
        state["link"]["issues"] = []
    else:
        state["start"] += 1
    with pytest.raises(PreflightError):
        if change in ("cli", "model"):
            asyncio.run(lms.LMStudioObserver(binding).idle())
        else:
            fit.check_service(binding)


def test_cli_mutation_during_command_is_rejected(studio, monkeypatch):
    original = lms._run

    def changed(command, **kwargs):
        result = original(command, **kwargs)
        studio[2].write_bytes(b"replaced observer")
        return result

    monkeypatch.setattr(lms, "_run", changed)
    with pytest.raises(PreflightError, match="cli_changed"):
        bind(studio)


def test_missing_cli_or_unexecutable_cli_fails_closed(studio):
    studio[2].chmod(0o600)
    with pytest.raises(PreflightError, match="cli_unavailable"):
        bind(studio)
    with pytest.raises(PreflightError, match="options_required"):
        macos.bind_service("lmstudio", studio[1], 321, "http://127.0.0.1:8080")


def test_other_engine_refuses_lms_options(native):
    with pytest.raises(PreflightError, match="lms_options_wrong_engine"):
        macos.bind_service("vllm", native[3], 321, "http://127.0.0.1:8080", lms_path="lms")


def test_declared_root_cannot_substitute_another_same_named_gguf(studio):
    _, model, cli, root = studio
    wrong_root = root.parent / "wrong-root"
    wrong_root.mkdir()
    other = wrong_root / model.name
    other.write_bytes(b"different weights with the same filename")
    with pytest.raises(PreflightError, match="model_file_not_observed"):
        fit.bind_service(
            "lmstudio",
            other,
            321,
            "http://127.0.0.1:8080",
            lms_path=cli,
            models_root=wrong_root,
            served_model="test-model",
        )


def test_model_mapping_disappearance_fails_later_native_check(studio):
    binding = bind(studio)
    studio[0]["rows"] = studio[0]["rows"][:-1]
    with pytest.raises(PreflightError, match="model_file_not_observed"):
        fit.check_service(binding)


def test_native_binding_cli_and_file_queries_share_the_same_total_deadline(studio, monkeypatch):
    clock, deadlines = [100.0], []
    original_query, original_files = lms._run, files.lsof_records
    monkeypatch.setattr(lms.time, "monotonic", lambda: clock[0])

    def slow_query(command, *, deadline):
        deadlines.append(deadline)
        result = original_query(command, deadline=deadline)
        clock[0] += 3.0
        return result

    def slow_files(pid, selectors, *, deadline):
        deadlines.append(deadline)
        result = original_files(pid, selectors, deadline=deadline)
        clock[0] += 3.0
        return result

    monkeypatch.setattr(lms, "_run", slow_query)
    monkeypatch.setattr(files, "lsof_records", slow_files)
    monkeypatch.setattr(macos, "unique_listener", lambda *args, **kwargs: None)
    with pytest.raises(PreflightError, match="observer_timeout"):
        bind(studio)
    assert deadlines == [110.0] * 4


@pytest.mark.skipif(os.name == "nt", reason="macOS-only observer subprocess")
def test_bounded_reader_rejects_large_output_timeout_and_malformed_json(monkeypatch):
    monkeypatch.setattr(lms, "_LIMIT", 32)
    with pytest.raises(PreflightError, match="output_too_large"):
        lms._run(["/bin/sh", "-c", "printf '%064d' 1"])
    with pytest.raises(PreflightError, match="unreadable"):
        lms._run(["/bin/sh", "-c", "printf '%s' '{\"x\":1,\"x\":2}'"])
    with pytest.raises(PreflightError, match="failed"):
        lms._run(["/bin/sh", "-c", "exit 2"])
    monkeypatch.setattr(lms, "_TIMEOUT", 0.02)
    with pytest.raises(PreflightError, match="timeout"):
        lms._run(["/bin/sh", "-c", "while :; do :; done"])
