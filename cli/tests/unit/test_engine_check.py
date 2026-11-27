"""Shared CLI/Studio engine_check wire contract and Darwin llama-server build checks."""

import json
from pathlib import Path

import jsonschema
import pytest

from inferyard.cli import main
from inferyard.contracts.schemas import export_schema
from inferyard.provisioning import preparation_io, runtime_verification
from inferyard.provisioning.runtime_profiles import RELEASE


def test_contract_golden_samples_match_the_schema():
    samples = json.loads(
        (
            Path(__file__).resolve().parents[3]
            / "studio"
            / "internal"
            / "inferyard"
            / "testdata"
            / "engine_check_contract.json"
        ).read_text()
    )
    assert samples
    for sample in samples:
        if sample["valid"]:
            jsonschema.validate(sample["response"], export_schema("engine_check"))
        else:
            with pytest.raises(jsonschema.ValidationError):
                jsonschema.validate(sample["response"], export_schema("engine_check"))


@pytest.mark.parametrize(
    "version_output,expected_release",
    [
        ("version: 12232 (f70247691)\nbuilt with Apple clang", "b12232-f70247691"),
        (
            "version: 0.7.0-dev (build 12232, commit f70247610)\nbuilt with AppleClang",
            "b12232-f70247610",
        ),
        ("llama-server prism-b12232-f702476\n", "prism-b12232-f702476"),
        ("version: 10743 (adfffbe41)\n", RELEASE),
    ],
)
def test_darwin_engine_check_accepts_new_and_pinned_llama_server_builds(
    capsys, tmp_path, monkeypatch, version_output, expected_release
):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    engine = bin_dir / "llama-server"
    engine.write_bytes(b"mach-o-binary")
    (bin_dir / "libggml-metal.dylib").write_bytes(b"mach-o-dylib")

    def fake_run(argv, **kwargs):
        assert argv == [str(engine.resolve()), "--version"]
        return type("Result", (), {"returncode": 0, "stdout": version_output, "stderr": ""})()

    monkeypatch.setattr(runtime_verification.subprocess, "run", fake_run)
    code = main(["engine", "check", "--engine", str(engine)])
    payload = json.loads(capsys.readouterr().out)
    jsonschema.validate(payload, export_schema("engine_check"))
    assert code == 0
    assert payload["details"]["eligible"] is True
    assert payload["details"]["expected_release"] == expected_release
    assert payload["details"]["modes"] == {"cpu": True, "metal": True}


def test_darwin_engine_check_rejects_unrecognized_version_output(capsys, tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    engine = bin_dir / "llama-server"
    engine.write_bytes(b"mach-o-binary")
    (bin_dir / "libggml-metal.dylib").write_bytes(b"mach-o-dylib")
    monkeypatch.setattr(
        runtime_verification.subprocess,
        "run",
        lambda *a, **k: type(
            "Result", (), {"returncode": 0, "stdout": "version: unknown", "stderr": ""}
        )(),
    )
    code = main(["engine", "check", "--engine", str(engine)])
    payload = json.loads(capsys.readouterr().out)
    jsonschema.validate(payload, export_schema("engine_check"))
    assert code == 0
    assert payload["details"]["eligible"] is False
    assert payload["details"]["reason"] == "unsupported_engine_build_requires_adapter_validation"
    assert payload["details"]["expected_release"] == RELEASE
