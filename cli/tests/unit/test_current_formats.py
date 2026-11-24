"""No historical source can regain admission through a current derived artifact."""

import hashlib
import json
from dataclasses import replace

import pytest

from inferyard.application.reporting import execute as compare
from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document, json_bytes
from inferyard.reporting.public_package import write_public
from tests.helpers import fixture_run as base_fixture_run


def fixture_run(root, **kwargs):
    def portable_config(path):
        loaded = load_config(path)
        config = loaded.config.to_dict()
        config["engine"]["startup_args"] = ["-m", config["model"]["local_path"]]
        return replace(loaded, config=Document.parse("config", config))

    with pytest.MonkeyPatch.context() as patched:
        patched.setattr("tests.helpers.load_config", portable_config)
        return base_fixture_run(root, **kwargs)


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
