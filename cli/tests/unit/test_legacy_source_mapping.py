"""Old artifacts remain explicit input errors through every locator entrypoint."""

import json
import shutil

import pytest

from inferyard.cli import main
from inferyard.contracts.validation import json_bytes
from tests.helpers import fixture_run


def assert_unsupported(out, mapping, version, supported, capsys, *, mapping_reason="unsupported_format"):
    before = {p.name: p.read_bytes() for p in out.iterdir()}
    assert main(["verify", "--path", str(out)]) == 2
    result = json.loads(capsys.readouterr().out)
    assert result["limitations"] == ["unsupported_format"]
    assert result["details"]["saved_version"] == version
    assert result["details"]["supported_versions"] == supported
    assert main(["verify", "--path", str(out), "--source-root", mapping]) == 2
    mapping_result = json.loads(capsys.readouterr().out)
    assert mapping_result["limitations"] == [mapping_reason]
    assert before == {p.name: p.read_bytes() for p in out.iterdir()}


@pytest.mark.parametrize("version", [1, 2, 3, 4])
def test_legacy_comparison_mapping_rejected_by_both_entrypoints(tmp_path, version, capsys):
    out = tmp_path / "comparison"
    out.mkdir()
    (out / "comparison.json").write_bytes(
        json_bytes({"schema_version": 3, "format_version": version})
    )
    (out / "index.json").write_bytes(json_bytes({"schema_version": 3, "report_format_version": 6}))
    if version == 4:
        # A retired comparison keeps its own version identity; it is not a report.
        assert main(["verify", "--path", str(out)]) == 2
        result = json.loads(capsys.readouterr().out)
        assert result["details"] == {
            "artifact": "comparison",
            "saved_version": 4,
            "supported_versions": [5],
        }
        assert main(
            ["verify", "--path", str(out), "--source-root", f"{tmp_path}=absent"]
        ) == 2
        assert json.loads(capsys.readouterr().out)["limitations"] == ["unsupported_format"]
        return
    assert_unsupported(
        out,
        f"{tmp_path / 'source'}={tmp_path / 'absent'}",
        version,
        [5],
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
        f"{tmp_path / 'source'}={tmp_path / 'absent'}",
        version,
        [],
        capsys,
        mapping_reason="invalid_input",
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
    for command, flag in (("verify", "--path"),):
        assert main([command, flag, str(out), "--source-root", f"{left}={destination}"]) == (
            0 if present else 4
        )
        capsys.readouterr()
