"""LM Studio native declaration and observer refusals, without running a model."""

import asyncio
import hashlib
import os
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms import engine_fit as fit
from inferyard.platforms import engine_fit_lmstudio as lms
from inferyard.platforms import engine_fit_lmstudio_files as files
from inferyard.platforms import engine_fit_macos as macos
from tests.unit import engine_fit_macos_helpers as mac_fixtures

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
        assert "--json" in command
        assert command[command.index("--port") + 1] == "8080"
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
