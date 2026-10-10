from copy import deepcopy

import pytest

from inferyard.cli import _request, parser
from inferyard.config.startup_portability import portable_startup
from inferyard.evidence.storage import EvidenceError


def config():
    return {
        "model": {"local_path": "/old/model", "template_path": "/old/template"},
        "engine": {
            "startup_args": [
                "-m",
                "/old/model",
                "--chat-template-file=/old/template",
                "--host",
                "127.0.0.1",
                "--port",
                "1234",
                "-t",
                "6",
                "--no-cache-prompt",
            ]
        },
    }


def test_local_rebinding_preserves_fingerprint_but_measurement_changes_do_not():
    before = config()
    after = deepcopy(before)
    after["model"].update(local_path="/new/model", template_path="/new/template")
    after["engine"]["startup_args"] = [
        "--model=/new/model",
        "--chat-template-file",
        "/new/template",
        "--host=localhost",
        "--port=4321",
        "-t",
        "6",
        "--no-cache-prompt",
    ]
    assert portable_startup(before) == portable_startup(after)
    after["engine"]["startup_args"][-2] = "4"
    assert portable_startup(before)["sha256"] != portable_startup(after)["sha256"]


@pytest.mark.parametrize("suffix", [["--model", "/old/model"], ["--port"], ["--host=0.0.0.0"]])
def test_ambiguous_bindings_are_rejected(suffix):
    value = config()
    value["engine"]["startup_args"] += suffix
    with pytest.raises(EvidenceError):
        portable_startup(value)


def test_wrong_artifact_binding_rejected():
    value = config()
    value["model"]["local_path"] = "/different/model"
    with pytest.raises(EvidenceError, match="artifact_binding"):
        portable_startup(value)


def test_public_source_check_is_offline_cli_without_endpoint_or_pid():
    request = _request(
        parser().parse_args(["verify", "--path", "/public", "--source-run", "/source"])
    )
    assert request.command == "verify"
    assert request.endpoint_url is None and request.server_pid is None
