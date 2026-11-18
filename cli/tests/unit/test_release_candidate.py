"""Release rejection paths use synthetic packages and never install or contact services."""

import io
import json
import subprocess
import sys
from pathlib import Path

import pytest
from inferyard.contracts.comparison_types import JSONRecord

from scripts.prepare_release_candidate import prepare
from scripts.release_common import (
    COMMANDS,
    EXIT_CODES,
    STUDIO_KIND,
    ApprovedFile,
    artifact_files,
    sha,
    studio_report_schema,
    wheel_resource_files,
)
from tests.unit.test_release_fixture import COMMIT, build_fixture


def _json_text(payload: JSONRecord) -> str:
    buffer = io.StringIO()
    json.dump(payload, buffer)
    return buffer.getvalue()


def _pinned_environment() -> JSONRecord:
    return {
        "python": "3.15.2",
        "tool_version": "0.0.5",
        "dependencies": {"pytest": None, "ruff": None, "idna": "3.20"},
    }


def _probe_process(argv: list[str], files: dict[str, ApprovedFile]) -> subprocess.CompletedProcess:
    payload = {
        "version": "0.0.5",
        "installed_root": "/synthetic",
        "tool_source_hash": sha(b"tool"),
        "scorer_hash": sha(b"scorer"),
        "resource_files": wheel_resource_files(files["wheel"].raw),
        "current_studio_report_kind": STUDIO_KIND,
        "unsupported_report_formats_rejected": list(range(1, 9)),
    }
    return subprocess.CompletedProcess(argv, 0, _json_text(payload), "")


def _schema_process(argv: list[str]) -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(argv, 0, _json_text(studio_report_schema()), "")


def _command_payload(name: str, pinned: JSONRecord) -> JSONRecord:
    if name == "verify":
        return {
            "command": "verify",
            "status": "error",
            "limitations": ["studio_report_not_verifiable"],
        }
    if name == "verify-legacy-report":
        return {"command": "verify", "status": "blocked", "limitations": ["unsupported_format"]}
    if name in ("different-versions", "persistent-tool-unchanged"):
        return {**pinned, "dependencies": {**pinned["dependencies"], "idna": "3.10"}}
    if name == "synthetic-upgrade-versions":
        return {**pinned, "tool_version": "0.0.6.dev0"}
    return pinned


def _drifted_member(name: str, raw: bytes, change: str) -> bytes:
    if name.endswith("/licenses/LICENSE") and change == "license_bytes":
        return b"changed license"
    if name.endswith("/METADATA") and change == "license_metadata":
        return raw.replace(b"License-Expression: MIT", b"License-Expression: Apache-2.0")
    if name.endswith("/METADATA") and change == "version_metadata":
        return raw.replace(b"Version: 0.0.5", b"Version: 0.0.6")
    if name.endswith("/entry_points.txt") and change == "entrypoint":
        return raw.replace(b"inferyard =", b"wrong-command =")
    return raw


def _assert_completed_matrix(
    result: JSONRecord, manifest: Path, files: dict[str, ApprovedFile]
) -> None:
    assert result["kind"] == "installed_safe_checks.v4"
    assert result["status"] == "passed"
    assert result["build_manifest_sha256"] == sha(manifest.read_bytes())
    assert result["artifact_sha256"] == {r: f.digest for r, f in files.items()}
    assert result["studio_report_schema"]["$id"] == studio_report_schema()["$id"]
    assert result["studio_json"]["schema_validated"] is True
    assert result["studio_json"]["verify_limitations"] == ["studio_report_not_verifiable"]
    assert set(result["probe_checks"]) == {"probe-install-a", "probe-install-b", "probe-rollback"}


def test_prepare_cli_and_verify_cli(tmp_path):
    manifest, _, installed, _ = build_fixture(tmp_path / "original")
    root = Path(__file__).resolve().parents[2]
    result = subprocess.run(
        [
            sys.executable,
            str(root / "scripts/prepare_release_candidate.py"),
            "--manifest",
            str(manifest),
            "--installed",
            str(installed),
            "--source-commit",
            COMMIT,
            "--out",
            str(tmp_path / "candidate"),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    response = json.loads(result.stdout)
    subprocess.run(
        [
            sys.executable,
            str(root / "scripts/verify_release_candidate.py"),
            "--manifest",
            response["manifest"],
            "--manifest-sha256",
            response["manifest_sha256"],
            "--source-commit",
            COMMIT,
            "--stage",
            str(tmp_path / "staged"),
        ],
        check=True,
        capture_output=True,
    )
    assert len(list((tmp_path / "staged").iterdir())) == 4


@pytest.mark.parametrize("change", ["dirty", "dev", "commit", "old", "checks", "license", "urls"])
def test_build_refusal(tmp_path: Path, change: str) -> None:
    manifest, data, installed, _ = build_fixture(tmp_path / "original")
    if change == "dirty":
        data["source_dirty"] = True
    elif change == "dev":
        data["version"] = "0.0.5.dev0"
    elif change == "commit":
        data["source_commit"] = "b" * 40
    elif change == "old":
        data["kind"] = "community_distribution.v1"
    elif change == "checks":
        data["checks"].pop("rebuilt_wheel")
    elif change == "license":
        data["license"] = "Apache-2.0"
    else:
        data["project_urls"] = {}
    with manifest.open("w", encoding="utf-8") as stream:
        json.dump(data, stream)
    with pytest.raises(ValueError):
        prepare(manifest, [installed], tmp_path / "candidate", COMMIT)
    assert not (tmp_path / "candidate").exists()


@pytest.mark.parametrize(
    "change", ["license_bytes", "license_metadata", "version_metadata", "entrypoint"]
)
def test_wheel_metadata_and_license_drift_are_rejected(tmp_path: Path, change: str) -> None:
    import zipfile

    manifest, data, installed, _ = build_fixture(tmp_path / "original")
    record = next(r for r in data["artifacts"] if r["role"] == "wheel")
    path = manifest.parent / record["path"]
    output = io.BytesIO()
    with zipfile.ZipFile(path) as source, zipfile.ZipFile(output, "w") as target:
        for name in source.namelist():
            target.writestr(name, _drifted_member(name, source.read(name), change))
    path.write_bytes(output.getvalue())
    record.update(bytes=len(output.getvalue()), sha256=sha(output.getvalue()))
    with manifest.open("w", encoding="utf-8") as stream:
        json.dump(data, stream)
    with pytest.raises(ValueError, match="license|metadata|entry_point"):
        prepare(manifest, [installed], tmp_path / "candidate", COMMIT)


def test_check_distribution_cli_produces_bound_v2_build(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture
) -> None:
    import tarfile

    from scripts import check_distribution

    manifest, data, installed, evidence = build_fixture(tmp_path / "original")
    checkout = tmp_path / "checkout"
    checkout.mkdir()
    wheel = next(r for r in data["artifacts"] if r["role"] == "wheel")
    sdist = next(r for r in data["artifacts"] if r["role"] == "sdist")
    with tarfile.open(manifest.parent / sdist["path"]) as archive:
        archive.extractall(checkout, filter="data")
    monkeypatch.setattr(check_distribution, "ROOT", checkout / "inferyard-0.0.5")
    monkeypatch.setattr(
        check_distribution.subprocess,
        "check_output",
        lambda argv, **_: COMMIT if "rev-parse" in argv else b"",
    )
    proof = manifest.parent / "constraints.txt.validation.json"
    proof.write_bytes((manifest.parent / "constraints.json").read_bytes())
    output = manifest.parent / "checked.json"
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "check_distribution",
            "--wheel",
            str(manifest.parent / wheel["path"]),
            "--sdist",
            str(manifest.parent / sdist["path"]),
            "--constraints",
            str(manifest.parent / "constraints.txt"),
            "--rebuilt-wheel",
            str(manifest.parent / wheel["path"]),
            "--out",
            str(output),
        ],
    )
    check_distribution.main()
    assert json.loads(capsys.readouterr().out)["candidate_status"] == "build_checked"
    evidence["build_manifest_sha256"] = sha(output.read_bytes())
    with installed.open("w", encoding="utf-8") as stream:
        json.dump(evidence, stream)
    prepare(output, [installed], tmp_path / "candidate", COMMIT)
