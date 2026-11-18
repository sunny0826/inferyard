"""Engine check uses fixture files and injected observations, never a real engine."""

import json
import subprocess
from pathlib import Path

import jsonschema
import pytest

from inferyard import cli
from inferyard.config import community_assets
from inferyard.provisioning import preparation_io
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.contracts.schemas import export_schema
from inferyard.provisioning import runtime_verification, windows_runtime_prepare
from inferyard.provisioning.runtime_profiles import RELEASE, profile_header
from tests.unit.test_community_runtime import synthetic_runtime as runtime_fixture

synthetic_runtime = runtime_fixture

VERSION_OK = {
    "argv": ["--version"],
    "returncode": 0,
    "stdout": "version: 10743 (adfffbe41)",
    "stderr": "",
}


def macos_engine(tmp_path, *, dylibs=("libggml.dylib",)):
    engine = tmp_path / "llama-server"
    engine.write_bytes(b"fixture, never executed")
    for name in dylibs:
        (tmp_path / name).write_bytes(b"fixture library")
    return engine


def run_cli(monkeypatch, capsys, argv, *, system=("Darwin", "arm64")):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: system)
    code = cli.main(argv)
    captured = capsys.readouterr()
    result = json.loads(captured.out)
    return code, result, captured


def test_windows_tampered_manifest_is_ineligible(synthetic_runtime, monkeypatch, capsys):
    request, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(request)
    Path(prepared["runtime_library_manifest"]).write_text("{}")
    monkeypatch.setattr(runtime_verification, "query_version", lambda *a, **k: dict(VERSION_OK))
    code = cli.main(["engine", "check", "--runtime-receipt", prepared["receipt"]])
    result = json.loads(capsys.readouterr().out)
    assert code == 0
    assert result["details"]["eligible"] is False
    assert result["details"]["reason"] == "runtime_asset_mismatch"
    assert result["details"]["mode"] is None


def test_contract_golden_samples_match_the_schema():
    # The same physical samples drive the Studio parser test
    # (studio/internal/inferyard/engine_check_golden_test.go): the CLI schema
    # must accept exactly the valid samples and reject every invalid one, so
    # neither side drifts from the shared contract.
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
