"""Public rejection behavior and metadata access without the live backend."""

import json
import os
import re
import subprocess
import sys
from pathlib import Path

import pytest

import inferyard.cli as cli
from inferyard.cli.request import build_request

CONFIG = object()
TOP_LEVEL_COMMANDS = (
    "catalogue",
    "compare",
    "compare-engines",
    "compare-plan",
    "config",
    "device-check",
    "engine",
    "engine-fit",
    "export",
    "extension",
    "filter-candidates",
    "init",
    "memory-check",
    "model",
    "overhead",
    "plan",
    "prepare-length",
    "probe",
    "progress",
    "public",
    "repeat-summary",
    "report",
    "rescore",
    "resume",
    "run",
    "runtime",
    "service",
    "speed-results",
    "verify",
)
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
