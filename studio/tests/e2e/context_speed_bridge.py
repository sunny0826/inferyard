"""Public CLI with localhost fake Prism; review/host fixtures are test-only."""

import json
import os
from pathlib import Path

from scripts.build_context_speed_bundle import BUNDLE_NAME, outputs
from tests.conftest import config_path  # noqa: F401
from tests.integration.context_speed_fixture import (  # noqa: F401
    scenario,
    tree_hashes,
)
from tests.integration.context_speed_fixture import (
    speed_service as speed_service_fixture,
)
from tests.integration.test_context_speed_cli import invoke, run_speed

speed_service = speed_service_fixture


def test_studio_context_speed_bridge(speed_service, monkeypatch, capsys):
    output = Path(os.environ["STUDIO_BRIDGE_OUTPUT"])
    output.mkdir(parents=True, exist_ok=False)
    accepted = speed_service["root"] / "draft-accepted"
    with monkeypatch.context() as draft:
        draft.setattr(
            "inferyard.bundles.registry.bundle_bytes", lambda _: outputs()[BUNDLE_NAME]
        )
        code, response = invoke(capsys, "init", "--bundle", "zh-context-speed", "--out", accepted)
    assert code == 0 and accepted.exists() and response["status"] == "prepared"
    refused = speed_service["root"] / "invalid-refused"
    broken = json.loads(outputs()[BUNDLE_NAME])
    broken["cases"] = []
    with monkeypatch.context() as invalid:
        invalid.setattr(
            "inferyard.bundles.registry.bundle_bytes",
            lambda _: json.dumps(broken).encode("utf-8"),
        )
        code, response = invoke(capsys, "init", "--bundle", "zh-context-speed", "--out", refused)
    (output / "validation-rejection.json").write_text(
        json.dumps({"exit_code": code, "response": response})
    )
    assert code == 4 and not refused.exists()
    assert response["status"] == "error" and response["details"] is None
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    before = tree_hashes(batch)
    calls = list(speed_service["calls"])
    code, result = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 0
    assert before == tree_hashes(batch)
    assert calls == speed_service["calls"]
    assert len(speed_service["requests"]) == 5
    assert not list(batch.rglob("*.html"))
    (output / "result.json").write_text(json.dumps(result))
    (output / "evidence.json").write_text(
        json.dumps(
            {
                "batch": str(batch),
                "exit_code": code,
                "request_count": 5,
                "read_added_requests": 0,
                "before": before,
                "after": tree_hashes(batch),
                "html_count": 0,
                "fixture_boundary": "synthetic review, host and localhost engine",
            }
        )
    )
