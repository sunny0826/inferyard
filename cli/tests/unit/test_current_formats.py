"""No historical source can regain admission through a current derived artifact."""

import hashlib
import importlib.util
import json

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.storage import json_bytes
from inferyard.reporting.comparison_report import execute as compare
from inferyard.reporting.public_package import verify_public, write_public
from inferyard.reporting.report import write_report
from tests.unit.test_verification import source_fixture as fixture_run


@pytest.mark.parametrize("version", [1, 2, 3, 4, 5, 6, 7, 9])
def test_report_unsupported_is_code_two_in_both_entries(tmp_path, capsys, version):
    (tmp_path / "index.json").write_bytes(
        json_bytes({"schema_version": 3, "report_format_version": version})
    )
    for args in (["verify", "--path"],):
        assert main([*args, str(tmp_path)]) == 2
        result = json.loads(capsys.readouterr().out)
        assert result["limitations"] == ["unsupported_format"]
        assert result["details"]["saved_version"] == version
        assert result["details"]["supported_versions"] == [8]
    assert {p.name for p in tmp_path.iterdir()} == {"index.json"}


@pytest.mark.parametrize("value", [None, True, "7", 7.0])
def test_malformed_report_version_is_integrity_error(tmp_path, value, capsys):
    (tmp_path / "index.json").write_bytes(
        json_bytes({"schema_version": 3, "report_format_version": value})
    )
    assert main(["verify", "--path", str(tmp_path)]) == 4
    capsys.readouterr()


def rewrite_source(root, name, change):
    path = root / name
    value = json.loads(path.read_bytes())
    change(value)
    raw = json_bytes(value)
    path.write_bytes(raw)
    manifest = json.loads((root / "manifest.json").read_bytes())
    manifest["files"][name].update(sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw))
    (root / "manifest.json").write_bytes(json_bytes(manifest))


@pytest.mark.parametrize("artifact", ["run", "report", "comparison", "public"])
def test_migrated_schema_three_rejected_recursively(tmp_path, capsys, artifact):
    root = fixture_run(tmp_path / "source")
    target = root
    if artifact == "report":
        target = tmp_path / "report"
        write_report([root], target)
    elif artifact == "comparison":
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


def test_bad_presentation_seal_precedes_unsupported(tmp_path, capsys):
    root = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    index = write_report([root], out)
    index["report_format_version"] = 2
    (out / "index.json").write_bytes(json_bytes(index))
    assert main(["verify", "--path", str(out)]) == 4
    assert json.loads(capsys.readouterr().out)["limitations"] == ["presentation_bytes_changed"]


@pytest.mark.parametrize("version", [1, 2, 3, 4])
def test_public_old_policy_rejected_after_hash_checks(tmp_path, version, capsys):
    from inferyard.evidence.storage import EvidenceError
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
    for name in ("migration", "migration_source", "migration_transform"):
        assert importlib.util.find_spec("inferyard.evidence." + name) is None
