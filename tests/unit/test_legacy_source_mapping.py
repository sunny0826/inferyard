"""Unsupported locator options are input errors, not successful ignored flags."""

import shutil

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import json_bytes
from inferyard.reporting.comparison_report import build_comparison
from inferyard.reporting.report import build_index
from inferyard.reporting.report_assets import template_name
from inferyard.reporting.report_common import _environment, render_report_html
from tests.helpers import fixture_run


def legacy_report(roots, out, *, version=6, comparison=None):
    index = build_index(roots, out, format_version=version, comparison_path=comparison)
    (out / "index.json").write_bytes(json_bytes(index))
    (out / "report.html").write_text(
        render_report_html(_environment(), template_name(version), index)
    )


@pytest.mark.parametrize("version", [1, 2, 3])
def test_legacy_comparison_mapping_rejected_by_both_entrypoints(tmp_path, version, capsys):
    roots = [fixture_run(tmp_path / name) for name in ("left", "right")]
    out = tmp_path / "comparison"
    out.mkdir()
    (out / "comparison.json").write_bytes(
        json_bytes(build_comparison(*roots, format_version=version))
    )
    legacy_report(roots, out, comparison=out)
    mapping = f"{roots[0]}={tmp_path / 'absent'}"
    for command, flag in (("compare-check", "--run"), ("verify", "--path")):
        args = [command, flag, str(out)]
        assert main(args) == 0
        assert main([*args, "--source-root", mapping]) == 2
        capsys.readouterr()
    (roots[0] / "events.jsonl").write_bytes(b"corrupt source")
    for command, flag in (("compare-check", "--run"), ("verify", "--path")):
        assert main([command, flag, str(out)]) == 4
        capsys.readouterr()


@pytest.mark.parametrize("version", [1, 6])
def test_legacy_report_mapping_remains_input_error_in_generic_verify(tmp_path, version, capsys):
    root = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    out.mkdir()
    legacy_report([root], out, version=version)
    for command, flag in (("report-check", "--run"), ("verify", "--path")):
        args = [command, flag, str(out)]
        assert main(args) == 0
        assert main([*args, "--source-root", f"{root}={tmp_path / 'absent'}"]) == 2
        capsys.readouterr()


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
