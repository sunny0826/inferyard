"""Public rejection behavior and metadata access without the live backend."""

import os
import subprocess
import sys
from pathlib import Path

CASES = [
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
    ["engine", "prepare", "--help"],
    ["assets", "scan", "--help"],
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
]

CLI_ROOT = Path(__file__).resolve().parents[2]

SCRIPT = """
import contextlib
import importlib.abc
import io
import json
import sys

sys.path.insert(0, sys.argv[1])

BLOCKED = ("inferyard.runtime", "inferyard.extensions", "inferyard.adapters")


class RejectLiveImports(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if any(fullname == prefix or fullname.startswith(prefix + ".") for prefix in BLOCKED):
            raise AssertionError("cold CLI imported " + fullname)


sys.meta_path.insert(0, RejectLiveImports())
from inferyard.cli import main

from tests.unit.test_cli_surface import CASES

failures = []
for arguments in CASES:
    out, err = io.StringIO(), io.StringIO()
    try:
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = main(arguments)
    except SystemExit as exc:
        code = exc.code if exc.code is not None else 0
    except BaseException as exc:  # Cold-start violation or parser failure.
        failures.append(" ".join([*arguments, type(exc).__name__ + ": " + str(exc)]))
        continue
    if code != 0 or err.getvalue():
        failures.append(" ".join([*arguments, str(code), err.getvalue()]))
    elif "--help" not in arguments and not isinstance(json.loads(out.getvalue()), dict):
        failures.append(" ".join([*arguments, "non_dict_json"]))

sys.stdout.write("\\n".join(failures))
"""


def test_all_help_and_metadata_are_cold_starts() -> None:
    """One isolated interpreter covers every command; no live backend may be imported."""
    result = subprocess.run(
        [sys.executable, "-c", SCRIPT, str(CLI_ROOT)],
        capture_output=True,
        text=True,
        check=False,
        env={**os.environ, "NO_COLOR": "1", "PYTHON_COLORS": "0"},
    )
    assert result.returncode == 0, (result.stdout, result.stderr)
    assert result.stdout == ""
    assert result.stderr == ""
