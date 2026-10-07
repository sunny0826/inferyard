"""Release rejection paths use synthetic packages and never install or contact services."""

import json
import subprocess
import sys
from pathlib import Path

import pytest

from scripts.prepare_release_candidate import prepare
from scripts.release_common import COMMANDS, artifact_files, sha
from tests.unit.test_release_fixture import COMMIT, build_fixture


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
def test_build_refusal(tmp_path, change):
    manifest, data, installed, _ = build_fixture(tmp_path / "original")
    if change == "dirty":
        data["source_dirty"] = True
    elif change == "dev":
        data["version"] = "0.0.1.dev0"
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
    manifest.write_text(json.dumps(data), encoding="utf-8")
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
        "commands",
        "scope",
        "arch",
        "old",
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
    elif change == "commands":
        evidence["checks"] = evidence["checks"][:-1]
    elif change == "scope":
        evidence["scope"] = ["offline_core"]
    elif change == "arch":
        evidence["architecture"] = "x86_64"
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


def test_synthetic_upgrade_is_next_development_version(tmp_path):
    import zipfile

    from tests.packaging.fixture_wheel import make_upgrade_fixture

    manifest, build, _, _ = build_fixture(tmp_path / "original")
    wheel = next(r for r in build["artifacts"] if r["role"] == "wheel")
    upgraded = make_upgrade_fixture(manifest.parent / wheel["path"], tmp_path)
    assert upgraded.name == "inferyard-0.0.2.dev0-py3-none-any.whl"
    with zipfile.ZipFile(upgraded) as archive:
        assert b'__version__ = "0.0.2.dev0"' in archive.read("inferyard/__init__.py")
        assert b"Version: 0.0.2.dev0" in archive.read("inferyard-0.0.2.dev0.dist-info/METADATA")


def test_simulated_completed_matrix_binds_actual_snapshots(tmp_path, monkeypatch):
    """Simulates subprocess outputs only; not an installed-platform acceptance record."""
    from tests.packaging import run_installed

    manifest, build, _, _ = build_fixture(tmp_path / "original")
    files = artifact_files(manifest.parent, build)
    fixtures = tmp_path / "fixtures"
    fixtures.mkdir()
    (fixtures / "expected.json").write_text('{"fixture_relative": "run"}', encoding="utf-8")
    out = tmp_path / "installed"
    monkeypatch.setattr(run_installed.shutil, "which", lambda name: sys.executable)
    monkeypatch.setattr(run_installed.sys, "platform", "darwin")
    monkeypatch.setattr(run_installed.platform, "machine", lambda: "arm64")
    pinned = {
        "python": "3.14.7",
        "tool_version": "0.0.1",
        "dependencies": {"pytest": None, "ruff": None, "idna": "3.20"},
    }
    calls = []

    def fake_run(argv, **kwargs):
        name = COMMANDS[len(calls)]
        calls.append(name)
        if name == "init":
            (out / "中文 workspace").mkdir()
            (out / "中文 workspace/preparation.json").write_bytes(b"frozen")
        if name == "different-versions" or name == "persistent-tool-unchanged":
            result = {**pinned, "dependencies": {**pinned["dependencies"], "idna": "3.10"}}
        elif name == "synthetic-upgrade-versions":
            result = {**pinned, "tool_version": "0.0.2.dev0"}
        else:
            result = pinned
        return subprocess.CompletedProcess(
            argv, 1 if name == "uvx-cold-offline" else 0, json.dumps(result), ""
        )

    monkeypatch.setattr(run_installed.subprocess, "run", fake_run)
    run_installed.run_matrix(
        manifest.parent / files["wheel"].path,
        manifest.parent / files["constraints"].path,
        fixtures,
        out,
        manifest,
    )
    result = json.loads((out / "result.json").read_bytes())
    assert result["status"] == "passed" and len(calls) == 26
    assert result["build_manifest_sha256"] == sha(manifest.read_bytes())
    assert result["artifact_sha256"] == {r: f.digest for r, f in files.items()}
    prepare(manifest, [out / "result.json"], tmp_path / "candidate", COMMIT)


@pytest.mark.parametrize(
    "change", ["license_bytes", "license_metadata", "version_metadata", "entrypoint"]
)
def test_wheel_metadata_and_license_drift_are_rejected(tmp_path, change):
    import io
    import zipfile

    manifest, data, installed, _ = build_fixture(tmp_path / "original")
    record = next(r for r in data["artifacts"] if r["role"] == "wheel")
    path = manifest.parent / record["path"]
    output = io.BytesIO()
    with zipfile.ZipFile(path) as source, zipfile.ZipFile(output, "w") as target:
        for name in source.namelist():
            raw = source.read(name)
            if name.endswith("/licenses/LICENSE") and change == "license_bytes":
                raw = b"changed license"
            elif name.endswith("/METADATA") and change == "license_metadata":
                raw = raw.replace(b"License-Expression: MIT", b"License-Expression: Apache-2.0")
            elif name.endswith("/METADATA") and change == "version_metadata":
                raw = raw.replace(b"Version: 0.0.1", b"Version: 0.0.2")
            elif name.endswith("/entry_points.txt") and change == "entrypoint":
                raw = raw.replace(b"inferyard =", b"wrong-command =")
            target.writestr(name, raw)
    path.write_bytes(output.getvalue())
    record.update(bytes=len(output.getvalue()), sha256=sha(output.getvalue()))
    manifest.write_text(json.dumps(data), encoding="utf-8")
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


def test_check_distribution_cli_produces_bound_v2_build(tmp_path, monkeypatch, capsys):
    import tarfile

    from scripts import check_distribution

    manifest, data, installed, evidence = build_fixture(tmp_path / "original")
    checkout = tmp_path / "checkout"
    checkout.mkdir()
    wheel = next(r for r in data["artifacts"] if r["role"] == "wheel")
    sdist = next(r for r in data["artifacts"] if r["role"] == "sdist")
    with tarfile.open(manifest.parent / sdist["path"]) as archive:
        archive.extractall(checkout, filter="data")
    monkeypatch.setattr(check_distribution, "ROOT", checkout / "inferyard-0.0.1")
    monkeypatch.setattr(
        check_distribution.subprocess,
        "check_output",
        lambda argv, **kw: COMMIT if "rev-parse" in argv else b"",
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
    installed.write_text(json.dumps(evidence), encoding="utf-8")
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
