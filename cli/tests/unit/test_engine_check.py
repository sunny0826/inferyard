"""Engine check uses fixture files and injected observations, never a real engine."""

import json
import subprocess
from pathlib import Path

import jsonschema
import pytest

from inferyard import cli
from inferyard.config import community_assets, preparation_io
from inferyard.config.preparation_io import PreparationError
from inferyard.contracts.schemas import export_schema
from inferyard.platforms import runtime_verification, windows_runtime_prepare
from inferyard.platforms.runtime_profiles import RELEASE, profile_header
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
    jsonschema.validate(result, export_schema("engine_check"))
    return code, result, captured


def test_macos_eligible_reports_dual_modes_and_writes_nothing(tmp_path, monkeypatch, capsys):
    engine = macos_engine(tmp_path)
    monkeypatch.setattr(runtime_verification, "query_version", lambda *a, **k: dict(VERSION_OK))
    code, result, _ = run_cli(monkeypatch, capsys, ["engine", "check", "--engine", str(engine)])
    assert code == 0
    assert result["status"] == "checked" and result["completeness"] == "complete"
    assert result["details"] == {
        "eligible": True,
        "reason": None,
        "reported_version": "version: 10743 (adfffbe41)",
        "expected_release": RELEASE,
        "modes": {"cpu": True, "metal": False},
        "mode": None,
    }
    assert sorted(path.name for path in tmp_path.iterdir()) == [
        "libggml.dylib",
        "llama-server",
    ]


def test_macos_metal_library_makes_metal_mode_available(tmp_path, monkeypatch, capsys):
    engine = macos_engine(tmp_path, dylibs=("libggml.dylib", "libggml-metal.dylib"))
    monkeypatch.setattr(runtime_verification, "query_version", lambda *a, **k: dict(VERSION_OK))
    code, result, _ = run_cli(monkeypatch, capsys, ["engine", "check", "--engine", str(engine)])
    assert code == 0
    assert result["details"]["eligible"] is True
    assert result["details"]["modes"] == {"cpu": True, "metal": True}
    assert result["details"]["mode"] is None


def test_macos_matching_version_output_is_eligible(tmp_path, monkeypatch, capsys):
    engine = macos_engine(tmp_path)

    def fake_run(argv, **kwargs):
        assert argv == [str(engine), "--version"]
        return subprocess.CompletedProcess(argv, 0, "version: 10743 (adfffbe41)", "")

    monkeypatch.setattr(subprocess, "run", fake_run)
    code, result, _ = run_cli(monkeypatch, capsys, ["engine", "check", "--engine", str(engine)])
    assert code == 0
    assert result["details"]["eligible"] is True
    assert result["details"]["reported_version"] == "version: 10743 (adfffbe41)"


def test_macos_version_mismatch_is_ineligible(tmp_path, monkeypatch, capsys):
    engine = macos_engine(tmp_path)

    def fake_run(argv, **kwargs):
        return subprocess.CompletedProcess(argv, 0, "version: 9999 (cafe)", "")

    monkeypatch.setattr(subprocess, "run", fake_run)
    code, result, captured = run_cli(
        monkeypatch, capsys, ["engine", "check", "--engine", str(engine)]
    )
    assert code == 0
    assert result["details"] == {
        "eligible": False,
        "reason": "unsupported_engine_build_requires_adapter_validation",
        "reported_version": None,
        "expected_release": RELEASE,
        "modes": None,
        "mode": None,
    }
    assert captured.err.strip() == (
        "engine ineligible: unsupported_engine_build_requires_adapter_validation"
    )


def test_macos_engine_without_dylibs_is_ineligible(tmp_path, monkeypatch, capsys):
    engine = macos_engine(tmp_path, dylibs=())
    monkeypatch.setattr(
        runtime_verification,
        "query_version",
        lambda *a, **k: pytest.fail("manifest check must precede the version query"),
    )
    code, result, _ = run_cli(monkeypatch, capsys, ["engine", "check", "--engine", str(engine)])
    assert code == 0
    assert result["details"]["eligible"] is False
    assert result["details"]["reason"] == "invalid_library_manifest"
    assert result["details"]["modes"] is None


def test_windows_eligible_reports_profile_mode(synthetic_runtime, monkeypatch, capsys):
    request, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(request)
    monkeypatch.setattr(runtime_verification, "query_version", lambda *a, **k: dict(VERSION_OK))
    code = cli.main(["engine", "check", "--runtime-receipt", prepared["receipt"]])
    result = json.loads(capsys.readouterr().out)
    jsonschema.validate(result, export_schema("engine_check"))
    assert code == 0
    assert result["details"] == {
        "eligible": True,
        "reason": None,
        "reported_version": "version: 10743 (adfffbe41)",
        "expected_release": RELEASE,
        "modes": None,
        "mode": "cuda",
    }


def test_windows_tampered_manifest_is_ineligible(synthetic_runtime, monkeypatch, capsys):
    request, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(request)
    Path(prepared["runtime_library_manifest"]).write_text("{}")
    monkeypatch.setattr(runtime_verification, "query_version", lambda *a, **k: dict(VERSION_OK))
    code = cli.main(["engine", "check", "--runtime-receipt", prepared["receipt"]])
    result = json.loads(capsys.readouterr().out)
    jsonschema.validate(result, export_schema("engine_check"))
    assert code == 0
    assert result["details"]["eligible"] is False
    assert result["details"]["reason"] == "runtime_asset_mismatch"
    assert result["details"]["mode"] is None


def test_windows_symlinked_receipt_is_ineligible_like_config_create(
    synthetic_runtime, tmp_path, monkeypatch, capsys
):
    # config create hands the original receipt path to the shared verifier,
    # which rejects symlinks; engine check must keep the same source of truth
    # instead of pre-resolving the path and silently accepting the link.
    request, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(request)
    linked_receipt = tmp_path / "linked-receipt.json"
    linked_receipt.symlink_to(prepared["receipt"])
    monkeypatch.setattr(runtime_verification, "query_version", lambda *a, **k: dict(VERSION_OK))
    code = cli.main(["engine", "check", "--runtime-receipt", str(linked_receipt)])
    result = json.loads(capsys.readouterr().out)
    jsonschema.validate(result, export_schema("engine_check"))
    assert code == 0
    assert result["details"]["eligible"] is False
    assert result["details"]["reason"] == "runtime_receipt_invalid"


def test_windows_unknown_profile_is_ineligible(synthetic_runtime, monkeypatch, capsys):
    request, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(request)
    receipt = json.loads(Path(prepared["receipt"]).read_text())
    receipt["profile"] = "unknown-profile"
    Path(prepared["receipt"]).write_text(json.dumps(receipt))

    def load(name):
        profile_header(name)
        raise AssertionError("unreachable")

    monkeypatch.setattr(runtime_verification, "load_profile", load)
    code = cli.main(["engine", "check", "--runtime-receipt", prepared["receipt"]])
    result = json.loads(capsys.readouterr().out)
    jsonschema.validate(result, export_schema("engine_check"))
    assert code == 0
    assert result["details"]["reason"] == "unsupported_runtime_profile"


@pytest.mark.parametrize(
    ("system", "argv"),
    [
        (("Darwin", "arm64"), ["engine", "check", "--runtime-receipt", "receipt.json"]),
        (("Windows", "x64"), ["engine", "check", "--engine", "llama-server.exe"]),
        (("Linux", "x64"), ["engine", "check", "--engine", "llama-server"]),
    ],
)
def test_platform_argument_mismatch_blocks_with_code_two(
    tmp_path, monkeypatch, capsys, system, argv
):
    code, result, _ = run_cli(monkeypatch, capsys, argv, system=system)
    assert code == 2
    assert result["status"] == "blocked"
    assert result["details"] is None
    assert result["limitations"] == [
        "unsupported_platform" if system[0] == "Linux" else "invalid_input"
    ]


@pytest.mark.parametrize(
    "argv",
    [
        ["engine", "check"],
        ["engine", "check", "--engine", "a", "--runtime-receipt", "b"],
    ],
)
def test_mutually_exclusive_arguments_reject_before_dispatch(argv, capsys):
    assert cli.main(argv) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_input"]


def test_failed_version_query_is_tool_error_without_raw_output(tmp_path, monkeypatch, capsys):
    engine = macos_engine(tmp_path)

    def fake_run(argv, **kwargs):
        return subprocess.CompletedProcess(argv, 1, "", "fixture-secret-7f39")

    monkeypatch.setattr(subprocess, "run", fake_run)
    code, result, captured = run_cli(
        monkeypatch, capsys, ["engine", "check", "--engine", str(engine)]
    )
    assert code == 4
    assert result["status"] == "error"
    assert result["limitations"] == ["engine_version_query_failed"]
    assert result["details"] is None
    assert "fixture-secret-7f39" not in captured.out + captured.err


@pytest.mark.parametrize("kind", ["missing", "directory", "non_directory_parent"])
@pytest.mark.parametrize(
    ("flag", "system"),
    [("--engine", ("Darwin", "arm64")), ("--runtime-receipt", ("Windows", "x64"))],
)
def test_invalid_input_path_is_blocked(tmp_path, monkeypatch, capsys, kind, flag, system):
    path = tmp_path / "missing"
    if kind == "directory":
        path = tmp_path
    elif kind == "non_directory_parent":
        parent = tmp_path / "file"
        parent.write_bytes(b"fixture")
        path = parent / "child"
    code, result, _ = run_cli(
        monkeypatch, capsys, ["engine", "check", flag, str(path)], system=system
    )
    assert code == 2
    assert result["status"] == "blocked"
    assert result["details"] is None
    assert result["limitations"] == ["invalid_input"]


def test_unexpected_failure_is_redacted_internal_error(tmp_path, monkeypatch, capsys):
    engine = macos_engine(tmp_path)

    def boom(*args, **kwargs):
        raise RuntimeError("fixture-secret-7f39")

    monkeypatch.setattr(community_assets, "libraries", boom)
    code, result, captured = run_cli(
        monkeypatch, capsys, ["engine", "check", "--engine", str(engine)]
    )
    assert code == 4
    assert result["limitations"] == ["internal_error"]
    assert "fixture-secret-7f39" not in captured.out + captured.err


def test_package_resource_failure_is_tool_error(synthetic_runtime, monkeypatch, capsys):
    request, _ = synthetic_runtime
    prepared = windows_runtime_prepare.prepare(request)

    def load(name):
        raise PreparationError("package_resource_invalid", 4)

    monkeypatch.setattr(runtime_verification, "load_profile", load)
    code = cli.main(["engine", "check", "--runtime-receipt", prepared["receipt"]])
    result = json.loads(capsys.readouterr().out)
    jsonschema.validate(result, export_schema("engine_check"))
    assert code == 4
    assert result["limitations"] == ["package_resource_invalid"]


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
