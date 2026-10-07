"""Canonical CLI inputs preserve request bindings and compatibility behavior."""

import json
import re
import socket
import subprocess
import sys
from dataclasses import fields
from pathlib import Path

import pytest

import inferyard.cli as cli
from inferyard.application.types import CommandRequest, CommandResult
from inferyard.cli.request import build_request

CONFIG = object()
LEGACY_INPUTS = [
    ["device-check", "--model", "model.gguf", "--models-root", "models", "--out", "out", "--json"],
    ["catalogue", "--kind", "methods"],
    ["plan", "--config", "experiment.json", "--dry-run"],
    ["check", "--config", "config.toml"],
    ["run", "--config", "config.toml"],
    ["resume", "--from-run", "source"],
    ["repeat-summary", "--run", "source", "--out", "out"],
    ["repeat-check", "--run", "source"],
    ["prepare-length", "--config", "config.toml", "--spec", "length.json", "--out", "out"],
    ["rescore", "--run", "source", "--out", "out", "--scorer", "s1", "--reason", "review"],
    ["rescore-check", "--run", "source"],
    ["export", "--analysis", "analysis", "--out", "out"],
    ["export-check", "--run", "source"],
    ["public-package", "--run", "source", "--out", "out"],
    ["public-check", "--run", "source", "--from-run", "parent"],
    ["public-config-check", "--run", "source", "--config", "config.toml"],
    ["public-plan", "--run", "source", "--config", "config.toml", "--out", "out"],
    ["report", "--run", "source", "--out", "out", "--comparison", "comparison"],
    ["report-check", "--run", "source"],
    ["compare", "--left", "a", "--right", "b", "--out", "out", "--left-overhead", "oh"],
    ["compare-check", "--run", "source"],
    ["filter-candidates", "--spec", "filter.json", "--out", "out"],
    [
        "overhead",
        "--plan",
        "plan.json",
        "--trial",
        "t1",
        "--output-root",
        "out",
        "--tolerance-ratio",
        ".05",
        "--max-wall-seconds",
        "60",
    ],
    ["overhead-check", "--run", "source", "--target-run", "target"],
    ["extension-freeze", "--spec", "spec.json", "--config", "config.toml", "--out", "out"],
    ["extension-run", "--spec", "plan.json", "--config", "config.toml", "--out", "out"],
    ["extension-replay", "--spec", "packet.json", "--out", "out"],
    ["extension-check", "--run", "source"],
]


def request(arguments):
    return build_request(cli.parser().parse_args(arguments), config_loader=lambda _: CONFIG)


@pytest.mark.parametrize("arguments", LEGACY_INPUTS, ids=lambda arguments: arguments[0])
def test_all_legacy_entries_preserve_the_called_command_and_result(arguments, monkeypatch, capsys):
    seen = []
    monkeypatch.setattr(cli, "load_config", lambda _: CONFIG)

    def handler(value):
        seen.append(value)
        return 0, CommandResult(value.command, "tested", "complete", details={"legacy": True})

    assert cli.main(arguments, handlers={arguments[0]: handler}) == 0
    output = capsys.readouterr()
    assert output.err == ""
    assert json.loads(output.out) == {
        "command": arguments[0],
        "status": "tested",
        "completeness": "complete",
        "run_id": None,
        "evidence_dir": None,
        "limitations": [],
        "details": {"legacy": True},
    }
    assert len(seen) == 1
    for field in fields(CommandRequest):
        value = getattr(seen[0], field.name)
        if isinstance(value, Path):
            assert value.is_absolute(), field.name
        elif isinstance(value, list):
            assert all(path.is_absolute() for path in value), field.name


@pytest.mark.parametrize(
    ("arguments", "command", "path_field"),
    [
        (["probe", "--config", "config.toml"], "probe", None),
        (["verify", "--path", "artifact"], "verify", "run"),
        (["public", "package", "--run", "source", "--out", "out"], "public package", "run"),
        (
            ["public", "plan", "--run", "source", "--config", "config.toml", "--out", "out"],
            "public plan",
            "run",
        ),
        (
            [
                "extension",
                "freeze",
                "--spec",
                "spec.json",
                "--config",
                "config.toml",
                "--out",
                "out",
            ],
            "extension freeze",
            "extension_spec",
        ),
        (
            ["extension", "run", "--plan", "plan.json", "--config", "config.toml", "--out", "out"],
            "extension run",
            "extension_spec",
        ),
        (
            ["extension", "replay", "--packet", "packet.json", "--out", "out"],
            "extension replay",
            "extension_spec",
        ),
    ],
)
def test_canonical_entries_dispatch_with_the_full_entry_name(
    arguments, command, path_field, monkeypatch, capsys
):
    seen = []
    monkeypatch.setattr(cli, "load_config", lambda _: CONFIG)

    def handler(value):
        seen.append(value)
        return 0, CommandResult(value.command, "tested")

    assert cli.main(arguments, handlers={command: handler}) == 0
    assert seen[0].command == command
    if path_field:
        assert getattr(seen[0], path_field).is_absolute()
    assert json.loads(capsys.readouterr().out)["command"] == command


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
        (
            ["extension-run"],
            "--plan",
            "--spec",
            ["--config", "cfg", "--out", "out"],
            "extension_spec",
        ),
        (["extension", "replay"], "--packet", "--spec", ["--out", "out"], "extension_spec"),
        (["extension-replay"], "--packet", "--spec", ["--out", "out"], "extension_spec"),
    ],
)
def test_parameter_aliases_produce_identical_requests(prefix, canonical, legacy, suffix, field):
    canonical_request = request([*prefix, canonical, "source", *suffix])
    assert canonical_request == request([*prefix, legacy, "source", *suffix])
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


def test_report_runs_accepts_one_or_more_paths_and_legacy_single_binding():
    single = request(["report", "--runs", "a", "--out", "out"])
    multiple = request(["report", "--runs", "a", "b", "--out", "out"])
    legacy = request(["report", "--run", "a", "--out", "out"])
    assert single.report_runs == [Path("a").resolve()]
    assert multiple.report_runs == [Path("a").resolve(), Path("b").resolve()]
    assert legacy.run == single.report_runs[0]
    assert legacy.report_runs is None


@pytest.mark.parametrize("command", [["public"], ["extension"]])
def test_command_groups_require_an_action(command, capsys):
    assert cli.main(command) == 2
    output = capsys.readouterr()
    assert json.loads(output.out)["limitations"] == ["invalid_input"]
    assert output.err


@pytest.mark.parametrize("code", [0, 2, 3, 4, 130])
def test_verify_source_binding_needs_no_service_and_preserves_exit_code(code, monkeypatch, capsys):
    def no_service(*args, **kwargs):
        raise AssertionError("artifact verification must not connect to a model service")

    monkeypatch.setattr(socket.socket, "connect", no_service)
    monkeypatch.setattr(cli, "load_config", lambda _: CONFIG)
    seen = []

    def handler(value):
        seen.append(value)
        return code, CommandResult(value.command, "tested")

    assert (
        cli.main(
            ["verify", "--path", "public", "--source-run", "source", "--config", "cfg"],
            handlers={"verify": handler},
        )
        == code
    )
    assert seen[0].run == Path("public").resolve()
    assert seen[0].from_run == Path("source").resolve()
    assert seen[0].config is CONFIG
    assert seen[0].endpoint_url is None and seen[0].server_pid is None
    assert json.loads(capsys.readouterr().out)["command"] == "verify"


def test_verify_target_path_is_bound_without_service():
    value = request(["verify", "--path", "overhead", "--target-run", "target"])
    assert value.run == Path("overhead").resolve()
    assert value.target_run == Path("target").resolve()
    assert value.endpoint_url is None and value.server_pid is None


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
        *[[value[0], "--help"] for value in LEGACY_INPUTS],
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
    )
    assert result.returncode == 0, (result.stdout, result.stderr)
    assert result.stderr == ""
    if arguments == ["--help"]:
        primary = re.findall(r"^    ([a-z][a-z-]+)\s+", result.stdout, flags=re.MULTILINE)
        assert len(primary) == 22
        assert {"verify", "probe", "public", "extension", "engine-fit"} <= set(primary)
        assert "check" not in primary and "extension-run" not in primary
        assert "Compatibility" in result.stdout and "extension-run" in result.stdout
    elif "--help" not in arguments:
        assert isinstance(json.loads(result.stdout), dict)
