"""CLI cold starts and application dispatch retain their public boundaries."""

import builtins
import json
import subprocess
import sys
from dataclasses import FrozenInstanceError, fields
from pathlib import Path
from types import ModuleType

import pytest

import inferyard.cli as cli
from inferyard.application.dispatch import default_backend
from inferyard.application.types import CommandRequest, CommandResult, Handler
from inferyard.contracts.validation import ContractError


@pytest.mark.parametrize("arguments", [["--help"], ["--versions"], ["--schema", "sample"]])
def test_metadata_cold_start_does_not_import_execution_backends(arguments):
    script = """
import importlib.abc
import json
import sys

blocked = (
    "inferyard.runtime",
    "inferyard.extensions",
    "inferyard.adapters",
)

class RejectBackends(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if any(fullname == prefix or fullname.startswith(prefix + ".") for prefix in blocked):
            raise AssertionError("metadata imported execution backend: " + fullname)

sys.meta_path.insert(0, RejectBackends())
from inferyard.cli import main
raise SystemExit(main(json.loads(sys.argv[1])))
"""
    completed = subprocess.run(
        [sys.executable, "-c", script, json.dumps(arguments)],
        capture_output=True,
        text=True,
        check=False,
    )
    assert completed.returncode == 0, (completed.stdout, completed.stderr)
    assert completed.stderr == ""
    if arguments == ["--help"]:
        assert "extension-run" not in completed.stdout
    else:
        assert isinstance(json.loads(completed.stdout), dict)


def test_cli_reexports_the_single_application_type_definitions():
    assert cli.CommandRequest is CommandRequest
    assert cli.CommandResult is CommandResult
    assert cli.Handler is Handler
    assert tuple(field.name for field in fields(CommandRequest)) == (
        "command",
        "config",
        "from_run",
        "run",
        "left",
        "right",
        "out",
        "diagnostic",
        "endpoint_url",
        "server_pid",
        "output_root",
        "api_key_env",
        "recovery_confirm",
        "recovery_note",
        "experiment",
        "dry_run",
        "catalogue_kind",
        "frozen_plan",
        "workload_id",
        "handoff_note",
        "trial_id",
        "tolerance_ratio",
        "max_wall_seconds",
        "comparison_mode",
        "filter_spec",
        "report_runs",
        "analysis_path",
        "scorer_id",
        "revision_reason",
        "length_spec",
        "target_run",
        "common_observer",
        "boundary_observer",
        "first_event_tolerance_ratio",
        "engine_rate_tolerance_ratio",
        "block_gap_tolerance_ms",
        "left_overhead",
        "right_overhead",
        "comparison_path",
        "extension_spec",
        "models_roots",
        "model_path",
        "fit_engines",
        "fit_engine",
        "fit_prompts",
        "fit_served_model",
        "fit_max_tokens",
        "fit_timeout",
        "fit_repetitions",
        "fit_min_memory_mib",
        "fit_lms_path",
        "fit_models_root",
        "fit_temperature_stop_override_reason",
        "fit_memory_stop_override_reason",
        "source_roots",
        "rerender",
        "community_bundle",
        "runtime_profile",
        "archive",
        "runtime_archive",
        "engine_path",
        "runtime_receipt",
        "preflight",
        "bundle_path",
        "candidate",
        "model_repo",
        "model_revision",
        "port",
        "model_source_url",
        "model_token_env",
        "model_source_record",
    )
    assert tuple(field.name for field in fields(CommandResult)) == (
        "command",
        "status",
        "completeness",
        "run_id",
        "evidence_dir",
        "limitations",
        "details",
    )
    for value in (CommandRequest("check"), CommandResult("check", "blocked")):
        assert not hasattr(value, "__dict__")
        with pytest.raises(FrozenInstanceError):
            value.command = "changed"


@pytest.mark.parametrize(
    ("command", "module", "planned"),
    [
        ("check", "inferyard.runtime.runner", False),
        ("run", "inferyard.runtime.runner", False),
        ("run", "inferyard.runtime.batch_runner", True),
        ("resume", "inferyard.runtime.batch_runner", False),
        ("device-check", "inferyard.platforms.device_preflight", False),
        ("extension-freeze", "inferyard.extensions.workflow", False),
        ("extension-run", "inferyard.extensions.workflow", False),
        ("extension-replay", "inferyard.extensions.workflow", False),
        ("extension-check", "inferyard.extensions.workflow", False),
        ("public-plan", "inferyard.config.public_plan", False),
        ("public-config-check", "inferyard.config.public_config_check", False),
        ("public-package", "inferyard.reporting.public_package", False),
        ("public-check", "inferyard.reporting.public_package", False),
        ("prepare-length", "inferyard.runtime.length_prepare", False),
        ("rescore", "inferyard.reporting.rescore", False),
        ("rescore-check", "inferyard.reporting.rescore", False),
        ("export", "inferyard.reporting.export", False),
        ("export-check", "inferyard.reporting.export", False),
        ("report", "inferyard.reporting.report", False),
        ("report-check", "inferyard.reporting.report", False),
        ("filter-candidates", "inferyard.analysis.candidate_filter", False),
        ("compare", "inferyard.reporting.comparison_report", False),
        ("compare-check", "inferyard.reporting.comparison_report", False),
        ("repeat-summary", "inferyard.reporting.repetition_report", False),
        ("repeat-check", "inferyard.reporting.repetition_report", False),
        ("overhead", "inferyard.application.overhead", True),
        ("overhead-check", "inferyard.application.overhead", False),
    ],
)
def test_explicit_dispatch_uses_the_selected_backend(command, module, planned, monkeypatch):
    calls = []
    backend = ModuleType(module)

    def execute(request):
        calls.append(request)
        return 0, CommandResult(request.command, "tested", "complete")

    backend.execute = execute
    monkeypatch.setitem(sys.modules, backend.__name__, backend)
    # Avoid a real host check when testing only the prepare-length route on Windows.
    monkeypatch.setattr("inferyard.application.dispatch.os", ModuleType("test_os"))
    monkeypatch.setattr("inferyard.application.dispatch.os.name", "posix", raising=False)
    request = CommandRequest(command, frozen_plan=Path("plan.json") if planned else None)
    assert default_backend(request) == (0, CommandResult(command, "tested", "complete"))
    assert calls == [request]


@pytest.mark.parametrize("command", ["fixture-secret-7f39", "extension-unknown"])
@pytest.mark.parametrize("frozen_plan", [None, Path("plan.json")])
def test_unknown_application_request_never_imports_a_fallback(command, frozen_plan, monkeypatch):
    importing = builtins.__import__

    def reject_fallback(name, *args, **kwargs):
        assert name not in (
            "inferyard.runtime.runner",
            "inferyard.runtime.batch_runner",
            "inferyard.extensions.workflow",
        )
        return importing(name, *args, **kwargs)

    monkeypatch.setattr(builtins, "__import__", reject_fallback)
    with pytest.raises(ContractError, match="unsupported command") as caught:
        default_backend(CommandRequest(command, frozen_plan=frozen_plan))
    assert command not in str(caught.value)


def test_public_load_config_hook_reaches_request_construction(monkeypatch, capsys):
    loaded = object()
    paths, calls = [], []

    def load(path):
        paths.append(path)
        return loaded

    def check(request):
        calls.append(request)
        return 2, CommandResult(request.command, "blocked")

    monkeypatch.setattr(cli, "load_config", load)
    assert cli.main(["probe", "--config", "synthetic-config"], handlers={"probe": check}) == 2
    assert paths == [Path("synthetic-config")]
    assert calls[0].config is loaded
    assert json.loads(capsys.readouterr().out)["status"] == "blocked"


@pytest.mark.parametrize("code", [0, 2, 3, 4, 130])
def test_entrypoint_preserves_supported_exit_codes(code, capsys):
    def handler(request):
        return code, CommandResult(request.command, "tested")

    assert cli.main(["catalogue", "--kind", "methods"], handlers={"catalogue": handler}) == code
    output = capsys.readouterr()
    assert output.err == ""
    assert len(output.out.splitlines()) == 1
    assert json.loads(output.out)["command"] == "catalogue"


@pytest.mark.parametrize("code", [1, 99])
def test_entrypoint_rejects_invalid_backend_exit_codes(code, capsys):
    def handler(request):
        return code, CommandResult(request.command, "fixture-secret-7f39")

    assert cli.main(["catalogue", "--kind", "methods"], handlers={"catalogue": handler}) == 4
    output = capsys.readouterr()
    assert "fixture-secret-7f39" not in output.out + output.err
    assert json.loads(output.out)["limitations"] == ["internal_error"]
