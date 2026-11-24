"""Installed preparation preserves authoritative packaged bundles without overwriting."""

import hashlib
import json
from pathlib import Path

import pytest

from inferyard.cli import main

ROOT = Path(__file__).parents[2]


def invoke(capsys, args):
    code = main(args)
    captured = capsys.readouterr()
    return code, json.loads(captured.out)


@pytest.mark.parametrize("name", ["zh-smoke", "zh-core", "zh-svg-pelican", "zh-context-speed"])
def test_init_preserves_authoritative_bytes_and_review(tmp_path, capsys, name):
    out = tmp_path / "中文 workspace"
    code, result = invoke(capsys, ["init", "--out", str(out), "--bundle", name])
    assert code == 0 and result["status"] == "prepared"
    assert result["evidence_dir"] is None and result["run_id"] is None
    source = (ROOT / f"bundles/{name}.json").read_bytes()
    assert (out / f"bundles/{name}.json").read_bytes() == source
    assert result["details"]["bundle_sha256"] == hashlib.sha256(source).hexdigest()
    assert result["details"]["bundles"] == [
        {
            "bundle": name,
            "bundle_path": result["details"]["bundle_path"],
            "bundle_sha256": result["details"]["bundle_sha256"],
        }
    ]
    assert result["details"]["ready_to_run"] is False
    assert set(result["details"]["configs"]) == {"linux", "macos", "windows"}
    assert not list((out / "results").iterdir())
    code, again = invoke(capsys, ["init", "--out", str(out)])
    assert code == 2 and again["limitations"] == ["output_exists"]
    assert (out / f"bundles/{name}.json").read_bytes() == source
