"""Public rejection behavior and metadata access without the live backend."""

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

import inferyard.cli as cli
from inferyard.cli.request import build_request

CONFIG = object()
REMOVED_COMMANDS = {
    "check",
    "overhead-check",
    "repeat-check",
    "rescore-check",
    "export-check",
    "public-check",
    "public-config-check",
    "report-check",
    "compare-check",
    "extension-check",
    "public-package",
    "public-plan",
    "extension-freeze",
    "extension-run",
    "extension-replay",
}


def request(arguments):
    return build_request(cli.parser().parse_args(arguments), config_loader=lambda _: CONFIG)


@pytest.mark.parametrize(
    ("prefix", "canonical", "legacy", "suffix", "field"),
    [
        (["plan"], "--experiment", "--config", ["--dry-run"], "experiment"),
        (
            ["run"],
            "--rerun-from",
            "--from-run",
            ["--endpoint-url", "http://127.0.0.1:8080", "--server-pid", "123"],
            "from_run",
        ),
        (
            ["extension", "run"],
            "--plan",
            "--spec",
            ["--config", "cfg", "--out", "out"],
            "extension_spec",
        ),
        (["extension", "replay"], "--packet", "--spec", ["--out", "out"], "extension_spec"),
    ],
)
def test_removed_parameter_aliases_are_rejected(prefix, canonical, legacy, suffix, field):
    canonical_request = request([*prefix, canonical, "source", *suffix])
    with pytest.raises(cli.ArgumentError):
        request([*prefix, legacy, "source", *suffix])
    assert getattr(canonical_request, field) == Path("source").resolve()


@pytest.mark.parametrize(
    "arguments",
    [
        ["plan", "--experiment", "a", "--config", "b", "--dry-run"],
        ["run", "--rerun-from", "a", "--from-run", "b"],
        ["report", "--runs", "a", "--run", "b", "--out", "out"],
        ["extension", "run", "--plan", "a", "--spec", "b", "--config", "cfg", "--out", "out"],
        ["extension-run", "--plan", "a", "--spec", "b", "--config", "cfg", "--out", "out"],
        ["extension", "replay", "--packet", "a", "--spec", "b", "--out", "out"],
        ["extension-replay", "--packet", "a", "--spec", "b", "--out", "out"],
    ],
)
@pytest.mark.parametrize("reverse", [False, True])
def test_conflicting_aliases_are_rejected_without_echoing_values(arguments, reverse, capsys):
    arguments = list(arguments)
    first = next(i for i, value in enumerate(arguments) if value.startswith("--"))
    second = first + 2
    arguments[first + 1] = "fixture-secret-7f39"
    if reverse:
        arguments[first : second + 2] = arguments[second : second + 2] + arguments[first:second]
    assert cli.main(arguments) == 2
    output = capsys.readouterr()
    assert "fixture-secret-7f39" not in output.out + output.err
    assert json.loads(output.out)["limitations"] == ["invalid_input"]


def test_report_runs_accepts_one_or_more_paths_and_rejects_old_flag():
    single = request(["report", "--runs", "a", "--out", "out"])
    multiple = request(["report", "--runs", "a", "b", "--out", "out"])
    with pytest.raises(cli.ArgumentError):
        request(["report", "--run", "a", "--out", "out"])
    assert single.report_runs == [Path("a").resolve()]
    assert multiple.report_runs == [Path("a").resolve(), Path("b").resolve()]


@pytest.mark.parametrize("command", [["public"], ["extension"]])
def test_command_groups_require_an_action(command, capsys):
    assert cli.main(command) == 2
    output = capsys.readouterr()
    assert json.loads(output.out)["limitations"] == ["invalid_input"]
    assert output.err


@pytest.mark.parametrize(
    "arguments",
    [
        ["run", "--rerun-from", "source"],
        ["run", "--rerun-from", "source", "--endpoint-url", "http://127.0.0.1:8080"],
        [
            "run",
            "--config",
            "cfg",
            "--endpoint-url",
            "http://127.0.0.1:8080",
            "--server-pid",
            "123",
        ],
        [
            "run",
            "--rerun-from",
            "source",
            "--endpoint-url",
            "http://127.0.0.1:8080",
            "--server-pid",
            "0",
        ],
        [
            "run",
            "--rerun-from",
            "source",
            "--endpoint-url",
            "http://example.com",
            "--server-pid",
            "123",
        ],
        ["run", "--plan", "plan.json"],
        ["resume", "--from-run", "source", "--server-pid", "123"],
    ],
)
def test_live_binding_rejections_still_block_dispatch(arguments, monkeypatch, capsys):
    seen = []
    monkeypatch.setattr(cli, "load_config", lambda _: CONFIG)
    assert cli.main(arguments, handlers={arguments[0]: seen.append}) == 2
    assert seen == []
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_input"]


@pytest.mark.parametrize(
    "arguments",
    [
        ["--help"],
        ["--versions"],
        ["--schema", "sample"],
        ["verify", "--help"],
        ["probe", "--help"],
        ["public", "--help"],
        ["public", "package", "--help"],
        ["public", "plan", "--help"],
        ["extension", "--help"],
        ["extension", "freeze", "--help"],
        ["extension", "run", "--help"],
        ["extension", "replay", "--help"],
        ["engine-fit", "--help"],
        ["engine-fit", "engines", "--help"],
        ["engine-fit", "plan", "--help"],
        ["engine-fit", "run", "--help"],
        ["engine-fit", "compare", "--help"],
        ["engine-fit", "verify", "--help"],
        ["init", "--help"],
        ["runtime", "prepare", "--help"],
        ["config", "assets", "--help"],
        ["config", "create", "--help"],
        ["config", "bind", "--help"],
        ["service", "--help"],
        ["service", "discover", "--help"],
        ["--schema", "service_discovery"],
        ["engine", "check", "--help"],
        ["--schema", "engine_check"],
        ["progress", "--help"],
        ["speed-results", "--help"],
        ["--schema", "context_speed_result"],
        *[
            [command, "--help"]
            for command in (
                "device-check",
                "catalogue",
                "plan",
                "run",
                "resume",
                "repeat-summary",
                "prepare-length",
                "rescore",
                "export",
                "report",
                "compare",
                "filter-candidates",
                "overhead",
            )
        ],
    ],
)
def test_all_help_and_metadata_are_cold_starts(arguments):
    script = """
import importlib.abc
import json
import sys

class RejectLiveImports(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        blocked = ("inferyard.runtime", "inferyard.extensions", "inferyard.adapters")
        if any(fullname == prefix or fullname.startswith(prefix + ".") for prefix in blocked):
            raise AssertionError("cold CLI imported " + fullname)

sys.meta_path.insert(0, RejectLiveImports())
from inferyard.cli import main
raise SystemExit(main(json.loads(sys.argv[1])))
"""
    result = subprocess.run(
        [sys.executable, "-c", script, json.dumps(arguments)],
        capture_output=True,
        text=True,
        check=False,
        env={**os.environ, "NO_COLOR": "1", "PYTHON_COLORS": "0"},
    )
    assert result.returncode == 0, (result.stdout, result.stderr)
    assert result.stderr == ""
    if "--help" not in arguments:
        assert isinstance(json.loads(result.stdout), dict)


@pytest.mark.parametrize("command", sorted(REMOVED_COMMANDS))
@pytest.mark.parametrize("suffix", [["--help"], ["--run", "fixture-secret-7f39"]])
def test_removed_commands_reject_before_dispatch(command, suffix, capsys):
    assert cli.main([command, *suffix]) == 2
    output = capsys.readouterr()
    assert "fixture-secret-7f39" not in output.out + output.err
    assert json.loads(output.out)["limitations"] == ["invalid_input"]
