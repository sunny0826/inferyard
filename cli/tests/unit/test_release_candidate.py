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
        "tool_version": "0.0.4",
        "dependencies": {"pytest": None, "ruff": None, "idna": "3.20"},
    }


def _probe_process(argv: list[str], files: dict[str, ApprovedFile]) -> subprocess.CompletedProcess:
    payload = {
        "version": "0.0.4",
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
        return {**pinned, "tool_version": "0.0.5.dev0"}
    return pinned


def _drifted_member(name: str, raw: bytes, change: str) -> bytes:
    if name.endswith("/licenses/LICENSE") and change == "license_bytes":
        return b"changed license"
    if name.endswith("/METADATA") and change == "license_metadata":
        return raw.replace(b"License-Expression: MIT", b"License-Expression: Apache-2.0")
    if name.endswith("/METADATA") and change == "version_metadata":
        return raw.replace(b"Version: 0.0.4", b"Version: 0.0.5")
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
        data["version"] = "0.0.4.dev0"
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
    "change",
    [
        "missing",
        "failed",
        "incomplete",
        "commit",
        "manifest",
        "artifact",
        "wheel",
        "commands",
        "scope",
        "arch",
        "old",
        "v3",
        "missing_probe",
        "missing_negative",
        "wrong_kind",
        "probe_stdout",
        "schema_id",
        "schema_digest",
        "studio_verify",
        "studio_limitations",
    ],
)
def test_installation_refusal(tmp_path, change):
    manifest, _, installed, evidence = build_fixture(tmp_path / "original")
    if change == "failed":
        evidence["status"] = "failed"
    elif change == "incomplete":
        evidence["completed"] = False
    elif change == "commit":
        evidence["source_commit"] = "b" * 40
    elif change == "manifest":
        evidence["build_manifest_sha256"] = "b" * 64
    elif change == "artifact":
        evidence["artifact_sha256"]["constraints"] = "b" * 64
    elif change == "wheel":
        evidence["wheel_sha256"] = "b" * 64
    elif change == "commands":
        evidence["checks"] = evidence["checks"][:-1]
    elif change == "scope":
        evidence["scope"] = ["offline_core"]
    elif change == "arch":
        evidence["architecture"] = "x86_64"
    elif change == "v3":
        evidence["kind"] = "installed_safe_checks.v3"
    elif change == "missing_probe":
        del evidence["probe_checks"]["probe-rollback"]
    elif change == "missing_negative":
        evidence["probe_checks"]["probe-install-b"][
            "unsupported_report_formats_rejected"
        ] = [1, 2]
    elif change == "wrong_kind":
        evidence["probe_checks"]["probe-install-a"]["current_studio_report_kind"] = "report.v8"
    elif change == "probe_stdout":
        evidence["probe_checks"]["probe-install-a"]["stdout_sha256"] = "not-a-digest"
    elif change == "schema_id":
        evidence["studio_report_schema"]["$id"] = "urn:local-ai-bench:schema:v3:report"
    elif change == "schema_digest":
        evidence["studio_report_schema"]["canonical_json_sha256"] = "b" * 64
    elif change == "studio_verify":
        evidence["studio_json"]["verify_exit_code"] = 0
    elif change == "studio_limitations":
        evidence["studio_json"]["verify_limitations"] = []
    elif change == "old":
        evidence["kind"] = "installed_safe_checks.v1"
    installed.write_text(json.dumps(evidence), encoding="utf-8")
    with pytest.raises(ValueError, match="installation_evidence"):
        prepare(manifest, [] if change == "missing" else [installed], tmp_path / "out", COMMIT)
    assert not (tmp_path / "out").exists()


def test_constraints_proof_is_content_checked(tmp_path):
    manifest, data, installed, evidence = build_fixture(tmp_path / "original")
    record = next(r for r in data["artifacts"] if r["role"] == "constraints_validation")
    raw = b"{}"
    (manifest.parent / record["path"]).write_bytes(raw)
    record.update(bytes=len(raw), sha256=sha(raw))
    manifest.write_text(json.dumps(data), encoding="utf-8")
    with pytest.raises(ValueError, match="constraints_validation_drift"):
        prepare(manifest, [installed], tmp_path / "out", COMMIT)


def test_run_installed_failure_cannot_write_passed_result(tmp_path, monkeypatch):
    from tests.packaging import run_installed

    manifest, build, _, _ = build_fixture(tmp_path / "original")
    files = artifact_files(manifest.parent, build)
    fixtures = tmp_path / "fixtures"
    fixtures.mkdir()
    monkeypatch.setattr(run_installed.shutil, "which", lambda name: sys.executable)
    monkeypatch.setattr(
        run_installed.subprocess,
        "run",
        lambda *a, **k: subprocess.CompletedProcess(a, 2, "", "injected failure"),
    )
    with pytest.raises(RuntimeError, match="install: expected"):
        run_installed.run_matrix(
            manifest.parent / files["wheel"].path,
            manifest.parent / files["constraints"].path,
            fixtures,
            tmp_path / "installed",
            manifest,
        )
    assert not (tmp_path / "installed/result.json").exists()


def test_synthetic_upgrade_is_next_development_version(tmp_path: Path) -> None:
    import zipfile

    from tests.packaging.fixture_wheel import make_upgrade_fixture

    manifest, build, _, _ = build_fixture(tmp_path / "original")
    wheel = next(r for r in build["artifacts"] if r["role"] == "wheel")
    upgraded = make_upgrade_fixture(manifest.parent / wheel["path"], tmp_path)
    assert upgraded.name == "inferyard-0.0.5.dev0-py3-none-any.whl"
    with zipfile.ZipFile(upgraded) as archive:
        assert b'__version__ = "0.0.5.dev0"' in archive.read("inferyard/__init__.py")
        assert b"Version: 0.0.5.dev0" in archive.read("inferyard-0.0.5.dev0.dist-info/METADATA")


def test_simulated_completed_matrix_binds_actual_snapshots(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Simulates subprocess outputs only; not an installed-platform acceptance record."""
    from tests.helpers import fixture_run
    from tests.packaging import run_installed

    manifest, build, _, _ = build_fixture(tmp_path / "original")
    files = artifact_files(manifest.parent, build)
    fixtures = tmp_path / "fixtures"
    fixtures.mkdir()
    synthetic = fixture_run(fixtures / "synthetic-runs")
    with (fixtures / "expected.json").open("w", encoding="utf-8") as stream:
        json.dump({"fixture_relative": synthetic.relative_to(fixtures).as_posix()}, stream)
    out = tmp_path / "installed"
    calls: list[str] = []
    pinned = _pinned_environment()

    def fake_run(argv: list[str], **_: str | int | bool | Path | dict[str, str]) -> subprocess.CompletedProcess:
        name = COMMANDS[len(calls)]
        calls.append(name)
        args = [str(item) for item in argv]
        if name.startswith("probe-"):
            return _probe_process(argv, files)
        if name == "schema-studio-report":
            return _schema_process(argv)
        if name == "init":
            (out / "中文 workspace").mkdir()
            (out / "中文 workspace/preparation.json").write_bytes(b"frozen")
        if name == "report":
            from inferyard.reporting.studio_report import write_studio_report

            roots = [Path(args[args.index("--runs") + 1])]
            write_studio_report(roots, Path(args[args.index("--out") + 1]))
        payload = _command_payload(name, pinned)
        return subprocess.CompletedProcess(argv, EXIT_CODES.get(name, 0), _json_text(payload), "")

    monkeypatch.setattr(run_installed.shutil, "which", lambda _: sys.executable)
    monkeypatch.setattr(run_installed.sys, "platform", "darwin")
    monkeypatch.setattr(run_installed.platform, "machine", lambda: "arm64")
    monkeypatch.setattr(run_installed.subprocess, "run", fake_run)
    run_installed.run_matrix(
        manifest.parent / files["wheel"].path,
        manifest.parent / files["constraints"].path,
        fixtures,
        out,
        manifest,
    )
    result = json.loads((out / "result.json").read_bytes())
    _assert_completed_matrix(result, manifest, files)
    prepare(manifest, [out / "result.json"], tmp_path / "candidate", COMMIT)


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


def test_symlink_artifact_is_rejected(tmp_path):
    manifest, data, installed, _ = build_fixture(tmp_path / "original")
    record = data["artifacts"][0]
    path = manifest.parent / record["path"]
    outside = tmp_path / "external.whl"
    path.rename(outside)
    path.symlink_to(outside)
    with pytest.raises(ValueError, match="candidate_symlink"):
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
    monkeypatch.setattr(check_distribution, "ROOT", checkout / "inferyard-0.0.4")
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


def test_build_refuses_dirty_source_before_creating_output(tmp_path, monkeypatch):
    from scripts import build_distribution

    monkeypatch.setattr(
        build_distribution.subprocess,
        "check_output",
        lambda argv, **kw: COMMIT.encode() if "rev-parse" in argv else b" M source.py",
    )
    monkeypatch.setattr(sys, "argv", ["build_distribution", "--out", str(tmp_path / "build")])
    with pytest.raises(ValueError, match="requires_clean_source"):
        build_distribution.main()
    assert not (tmp_path / "build").exists()
