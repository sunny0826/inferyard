"""Canonical command groups preserve the existing specialized execution behavior."""

import sys
from types import ModuleType

import pytest

from inferyard.application.dispatch import default_backend
from inferyard.application.types import CommandRequest, CommandResult


@pytest.mark.parametrize(
    ("canonical", "legacy", "module"),
    [
        ("probe", "check", "inferyard.runtime.runner"),
        ("public package", "public-package", "inferyard.reporting.public_package"),
        ("public plan", "public-plan", "inferyard.config.public_plan"),
        ("extension freeze", "extension-freeze", "inferyard.extensions.workflow"),
        ("extension run", "extension-run", "inferyard.extensions.workflow"),
        ("extension replay", "extension-replay", "inferyard.extensions.workflow"),
    ],
)
@pytest.mark.parametrize("code", [0, 2, 3, 4, 130])
def test_grouped_command_preserves_backend_result(canonical, legacy, module, code, monkeypatch):
    calls = []
    backend = ModuleType(module)

    def execute(request):
        calls.append(request)
        return code, CommandResult(
            legacy,
            "fixture_status",
            "incomplete",
            limitations=("fixture_limitation",),
            details={"ready_to_run": False},
        )

    backend.execute = execute
    monkeypatch.setitem(sys.modules, module, backend)
    request = CommandRequest(canonical, diagnostic=True, handoff_note="fixture")
    result_code, result = default_backend(request)
    assert result_code == code
    assert result == CommandResult(
        canonical,
        "fixture_status",
        "incomplete",
        limitations=("fixture_limitation",),
        details={"ready_to_run": False},
    )
    assert len(calls) == 1
    assert calls[0].command == legacy
    assert calls[0].diagnostic and calls[0].handoff_note == "fixture"
    assert request.command == canonical


def test_verify_dispatch_does_not_fall_through_to_live_runner(monkeypatch):
    module = ModuleType("inferyard.application.verification")
    calls = []

    def execute(request):
        calls.append(request)
        return 4, CommandResult("verify", "error", limitations=("evidence_error",))

    module.execute = execute
    monkeypatch.setitem(sys.modules, module.__name__, module)
    request = CommandRequest("verify")
    assert default_backend(request)[0] == 4
    assert calls == [request]
