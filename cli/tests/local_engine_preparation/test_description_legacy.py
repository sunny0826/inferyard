"""Legacy descriptions consume validated fixture configs, never engine observations."""

import json

import pytest

from inferyard import cli
from inferyard.application.types import CommandRequest
from inferyard.config.loader import load_config
from inferyard.config.local_engines import inspection


def test_legacy_description_preserves_launch_and_declared_assets(config_path, monkeypatch):
    from inferyard.platforms import runtime_verification

    monkeypatch.setattr(
        runtime_verification,
        "query_version",
        lambda *a, **kw: pytest.fail("description must not query an engine"),
    )
    monkeypatch.setattr(
        inspection, "assets", lambda *a: pytest.fail("legacy must not inspect local installation")
    )
    config = load_config(config_path).config.to_dict()
    result = inspection.describe(CommandRequest("config describe", candidate=config_path))
    assert result["definition"] == "engine-description.v1"
    assert result["adapter"] == config["engine"]["adapter"]
    assert result["launch"] == {
        "kind": "direct_process",
        "argv": [config["engine"]["binary_path"], *config["engine"]["startup_args"]],
        "working_directory": None,
        "environment": None,
        "ownership": "operator",
    }
    for name, section, path, digest in (
        ("engine", "engine", "binary_path", "binary_sha256"),
        ("template", "model", "template_path", "template_sha256"),
    ):
        assert result["assets"][name] == {
            "path": config[section][path],
            "sha256": config[section][digest],
        }
    assert result["assets"]["model"]["content_sha256"] == config["model"]["sha256"]
    assert result["model_requests_sent"] == 0
    assert result["endpoint"] == config["endpoint"]
    assert result["conditions"] == config["conditions"]
    assert result["generation"] == config["generation"]


def test_legacy_description_cli_envelope(config_path, capsys):
    assert cli.main(["config", "describe", "--candidate", str(config_path)]) == 0
    response = json.loads(capsys.readouterr().out)
    assert response["command"] == "config describe"
    assert response["status"] == "prepared"
    assert response["completeness"] == "complete"
    assert response["details"]["definition"] == "engine-description.v1"
