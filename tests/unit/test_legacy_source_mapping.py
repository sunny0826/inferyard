"""Old artifacts remain explicit input errors through every locator entrypoint."""

import json
import shutil

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import json_bytes
from tests.helpers import fixture_run


def assert_unsupported(out, commands, mapping, version, supported, capsys):
    before = {p.name: p.read_bytes() for p in out.iterdir()}
    for command, flag in commands:
        for options in ([], ["--source-root", mapping]):
            assert main([command, flag, str(out), *options]) == 2
            result = json.loads(capsys.readouterr().out)
            assert result["limitations"] == ["unsupported_format"]
            assert result["details"]["saved_version"] == version
            assert result["details"]["supported_versions"] == [supported]
            assert before == {p.name: p.read_bytes() for p in out.iterdir()}


@pytest.mark.parametrize("version", [1, 2, 3])
def test_legacy_comparison_mapping_rejected_by_both_entrypoints(tmp_path, version, capsys):
    out = tmp_path / "comparison"
    out.mkdir()
    (out / "comparison.json").write_bytes(
        json_bytes({"schema_version": 3, "format_version": version})
    )
    (out / "index.json").write_bytes(json_bytes({"schema_version": 3, "report_format_version": 6}))
    assert_unsupported(
        out,
        (("compare-check", "--run"), ("verify", "--path")),
        f"{tmp_path / 'source'}={tmp_path / 'absent'}",
        version,
        4,
        capsys,
    )


@pytest.mark.parametrize("version", [1, 6])
def test_legacy_report_mapping_rejected_by_both_entrypoints(tmp_path, version, capsys):
    out = tmp_path / "report"
    out.mkdir()
    (out / "index.json").write_bytes(
        json_bytes({"schema_version": 3, "report_format_version": version})
    )
    assert_unsupported(
        out,
        (("report-check", "--run"), ("verify", "--path")),
        f"{tmp_path / 'source'}={tmp_path / 'absent'}",
        version,
        7,
        capsys,
    )


@pytest.mark.parametrize("present", [True, False])
def test_modern_comparison_mapping_is_applied_and_bad_source_is_evidence_error(
    tmp_path, present, capsys
):
    left, right = [fixture_run(tmp_path / name) for name in ("left", "right")]
    out = tmp_path / "comparison"
    assert main(["compare", "--left", str(left), "--right", str(right), "--out", str(out)]) == 0
    capsys.readouterr()
    destination = tmp_path / "relocated"
    if present:
        shutil.move(left, destination)
    for command, flag in (("compare-check", "--run"), ("verify", "--path")):
        assert main([command, flag, str(out), "--source-root", f"{left}={destination}"]) == (
            0 if present else 4
        )
        capsys.readouterr()
