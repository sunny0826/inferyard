"""CLI metadata and exit codes remain usable without execution dependencies."""

import json
import subprocess
import sys

import pytest

import inferyard.cli as cli
from inferyard.application.types import CommandResult


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
    "inferyard.platforms",
    "inferyard.provisioning",
    "inferyard.evidence",
    "inferyard.analysis",
    "inferyard.config",
    "inferyard.reporting",
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
        return code, CommandResult(request.command, "synthetic-private-value")

    assert cli.main(["catalogue", "--kind", "methods"], handlers={"catalogue": handler}) == 4
    output = capsys.readouterr()
    assert "synthetic-private-value" not in output.out + output.err
    assert json.loads(output.out)["limitations"] == ["internal_error"]
