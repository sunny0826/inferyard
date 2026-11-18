"""No historical source can regain admission through a current derived artifact."""

import hashlib
import json

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError, UnsupportedFormat
from inferyard.application.reporting import execute as compare
from inferyard.reporting.public_package import verify_public, write_public
from inferyard.reporting.studio_report import write_studio_report
from tests.unit.legacy_presentation_helpers import legacy_layout
from tests.unit.test_verification import source_fixture as fixture_run


def directory_files(root):
    return {p.name for p in root.iterdir()}


def rewrite_source(root, name, change):
    path = root / name
    value = json.loads(path.read_bytes())
    change(value)
    raw = json_bytes(value)
    path.write_bytes(raw)
    manifest = json.loads((root / "manifest.json").read_bytes())
    manifest["files"][name].update(sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw))
    (root / "manifest.json").write_bytes(json_bytes(manifest))


@pytest.mark.parametrize("artifact", ["run", "comparison", "public"])
def test_migrated_schema_three_rejected_recursively(tmp_path, capsys, artifact):
    root = fixture_run(tmp_path / "source")
    target = root
    if artifact == "comparison":
        target = tmp_path / "comparison"
        compare(
            CommandRequest("compare", left=root, right=fixture_run(tmp_path / "other"), out=target)
        )
    elif artifact == "public":
        target = tmp_path / "public"
        write_public(root, target)
    rewrite_source(root, "run.json", lambda value: value.update(origin="migrated"))
    manifest = json.loads((root / "manifest.json").read_bytes())
    manifest["origin"] = "migrated"
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    args = ["verify", "--path", str(target)]
    if artifact == "public":
        args += ["--source-run", str(root)]
    assert main(args) == 2
    assert "unsupported_format" in json.loads(capsys.readouterr().out)["limitations"]
