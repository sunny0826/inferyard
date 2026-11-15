"""No historical source can regain admission through a current derived artifact."""

import hashlib
import json

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError, UnsupportedFormat
from inferyard.application.reporting import execute as compare
from inferyard.reporting.public_package import verify_public, write_public
from inferyard.reporting.studio_report import write_studio_report
from tests.unit.test_legacy_presentation_seal import legacy_layout
from tests.unit.test_verification import source_fixture as fixture_run


def directory_files(root):
    return {p.name for p in root.iterdir()}


@pytest.mark.parametrize("version", [1, 2, 3, 4, 5, 6, 7, 8, 9])
def test_retired_report_is_unsupported_with_empty_support(tmp_path, capsys, version):
    legacy_layout(tmp_path, version=version, seal=False)
    before = directory_files(tmp_path)
    assert main(["verify", "--path", str(tmp_path)]) == 2
    result = json.loads(capsys.readouterr().out)
    assert result["limitations"] == ["unsupported_format"]
    assert result["details"] == {
        "artifact": "report",
        "saved_version": version,
        "supported_versions": [],
    }
    assert directory_files(tmp_path) == before


@pytest.mark.parametrize("value", [None, True, "7", 7.0])
def test_malformed_report_version_is_integrity_error(tmp_path, value, capsys):
    legacy_layout(tmp_path, seal=False)
    (tmp_path / "index.json").write_bytes(
        json_bytes({"schema_version": 3, "report_format_version": value})
    )
    assert main(["verify", "--path", str(tmp_path)]) == 4
    capsys.readouterr()


def test_complete_sealed_legacy_report_is_still_only_a_version_rejection(tmp_path, capsys):
    legacy_layout(tmp_path)
    assert main(["verify", "--path", str(tmp_path)]) == 2
    result = json.loads(capsys.readouterr().out)
    assert result["details"]["supported_versions"] == []


def test_legacy_report_missing_html_is_an_integrity_error(tmp_path, capsys):
    legacy_layout(tmp_path)
    (tmp_path / "report.html").unlink()
    assert main(["verify", "--path", str(tmp_path)]) == 4
    capsys.readouterr()


def test_corrupt_legacy_report_json_is_an_integrity_error(tmp_path, capsys):
    legacy_layout(tmp_path, seal=False)
    (tmp_path / "index.json").write_bytes(b"{broken")
    assert main(["verify", "--path", str(tmp_path)]) == 4
    capsys.readouterr()


def test_current_studio_report_is_never_verifiable(tmp_path, capsys):
    root = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    write_studio_report([root], out)
    assert main(["verify", "--path", str(out)]) == 4
    result = json.loads(capsys.readouterr().out)
    assert result["status"] == "error"
    assert result["limitations"] == ["studio_report_not_verifiable"]
    assert directory_files(out) == {"studio.json"}


def test_studio_document_must_own_its_directory(tmp_path, capsys):
    root = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    write_studio_report([root], out)
    (out / "index.json").write_text("{}")
    assert main(["verify", "--path", str(out)]) == 4
    capsys.readouterr()


def test_current_comparison_owns_one_file_and_rejects_retired_layout(tmp_path, capsys):
    left = fixture_run(tmp_path / "left")
    right = fixture_run(tmp_path / "right")
    out = tmp_path / "comparison"
    compare(CommandRequest("compare", left=left, right=right, out=out))
    assert directory_files(out) == {"comparison.json"}
    assert main(["verify", "--path", str(out)]) == 0
    capsys.readouterr()
    (out / "report.html").write_text("retired presentation")
    assert main(["verify", "--path", str(out)]) == 4
    result = json.loads(capsys.readouterr().out)
    assert result["limitations"] == ["comparison_format_invalid"]


def test_retired_comparison_format_is_rejected_by_version(tmp_path, capsys):
    legacy_layout(tmp_path, seal=False, comparison=True)
    assert main(["verify", "--path", str(tmp_path)]) == 2
    result = json.loads(capsys.readouterr().out)
    assert result["limitations"] == ["unsupported_format"]
    assert result["details"] == {
        "artifact": "comparison",
        "saved_version": 4,
        "supported_versions": [5],
    }


def test_bad_presentation_seal_precedes_comparison_version_rejection(tmp_path, capsys):
    legacy_layout(tmp_path, comparison=True)
    (tmp_path / "comparison.json").write_bytes(
        json_bytes({"schema_version": 3, "format_version": 4, "changed": True})
    )
    assert main(["verify", "--path", str(tmp_path)]) == 4
    result = json.loads(capsys.readouterr().out)
    assert result["limitations"] == ["presentation_bytes_changed"]


def rewrite_source(root, name, change):
    path = root / name
    value = json.loads(path.read_bytes())
    change(value)
    raw = json_bytes(value)
    path.write_bytes(raw)
    manifest = json.loads((root / "manifest.json").read_bytes())
    manifest["files"][name].update(sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw))
    (root / "manifest.json").write_bytes(json_bytes(manifest))


@pytest.mark.parametrize("artifact", ["run", "comparison", "public"])
def test_migrated_schema_three_rejected_recursively(tmp_path, capsys, artifact):
    root = fixture_run(tmp_path / "source")
    target = root
    if artifact == "comparison":
        target = tmp_path / "comparison"
        compare(
            CommandRequest("compare", left=root, right=fixture_run(tmp_path / "other"), out=target)
        )
    elif artifact == "public":
        target = tmp_path / "public"
        write_public(root, target)
    rewrite_source(root, "run.json", lambda value: value.update(origin="migrated"))
    manifest = json.loads((root / "manifest.json").read_bytes())
    manifest["origin"] = "migrated"
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    args = ["verify", "--path", str(target)]
    if artifact == "public":
        args += ["--source-run", str(root)]
    assert main(args) == 2
    assert "unsupported_format" in json.loads(capsys.readouterr().out)["limitations"]


@pytest.mark.parametrize("version", [1, 2, 9])
def test_old_core_manifest_hash_checked_before_version(tmp_path, capsys, version):
    root = fixture_run(tmp_path / "source")
    manifest = json.loads((root / "manifest.json").read_bytes())
    manifest["schema_version"] = version
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    assert main(["verify", "--path", str(root)]) == 2
    capsys.readouterr()
    with (root / "events.jsonl").open("ab") as stream:
        stream.write(b"broken\n")
    assert main(["verify", "--path", str(root)]) == 4
    capsys.readouterr()


@pytest.mark.parametrize("version", [1, 2, 3, 4])
def test_public_old_policy_rejected_after_hash_checks(tmp_path, version, capsys):
    from tests.unit.test_verification import digest_tree

    root = fixture_run(tmp_path / "source")
    before_source = digest_tree(root)
    out = tmp_path / "public"
    write_public(root, out)
    assert verify_public(out)["integrity_verified"]

    def check_cli(code, reason):
        before = digest_tree(out)
        for command, flag in (("verify", "--path"),):
            assert main([command, flag, str(out)]) == code
            result = json.loads(capsys.readouterr().out)
            if reason is not None:
                assert result["limitations"] == [reason]
            assert digest_tree(out) == before
        assert digest_tree(root) == before_source

    check_cli(0, None)
    candidate = json.loads((out / "candidate.json").read_bytes())
    candidate.update(policy=f"public-summary.v{version}", format_version=version)
    raw = json_bytes(candidate)
    (out / "candidate.json").write_bytes(raw)
    manifest = json.loads((out / "manifest.json").read_bytes())
    manifest.update(policy=candidate["policy"])
    manifest["files"]["candidate.json"] = hashlib.sha256(raw).hexdigest()
    (out / "manifest.json").write_bytes(json_bytes(manifest))
    # File seals match, but the existing canonical candidate self-hash is now wrong.
    with pytest.raises(EvidenceError, match="public_candidate_identity_mismatch"):
        verify_public(out)
    check_cli(4, "public_candidate_identity_mismatch")

    body = {key: value for key, value in candidate.items() if key != "candidate_id"}
    candidate["candidate_id"] = "candidate-" + hashlib.sha256(json_bytes(body)).hexdigest()[:32]
    raw = json_bytes(candidate)
    (out / "candidate.json").write_bytes(raw)
    manifest["candidate_id"] = candidate["candidate_id"]
    manifest["files"]["candidate.json"] = hashlib.sha256(raw).hexdigest()
    (out / "manifest.json").write_bytes(json_bytes(manifest))
    with pytest.raises(UnsupportedFormat):
        verify_public(out)
    check_cli(2, "unsupported_format")
    (out / "REPRODUCE.md").write_bytes(b"changed")
    with pytest.raises(EvidenceError, match="public_hash_mismatch"):
        verify_public(out)


def test_migrator_command_and_modules_are_absent(capsys):
    assert main(["migrate", "--run", "missing", "--out", "missing"]) == 2
    capsys.readouterr()
