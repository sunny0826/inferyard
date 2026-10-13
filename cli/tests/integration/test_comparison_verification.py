"""Real comparison products reject bool/numeric substitutions through both CLIs."""

import asyncio
import json
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.reporting.comparison_report import read_verified_comparison
from inferyard.runtime.runner import execute_async
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


@pytest.fixture
def comparison(scenario, tmp_path, capsys):
    request, deps, _, _ = scenario
    roots = [Path(asyncio.run(execute_async(request, deps))[1].evidence_dir) for _ in range(2)]
    out = tmp_path / "comparison"
    assert (
        main(["compare", "--left", str(roots[0]), "--right", str(roots[1]), "--out", str(out)]) == 0
    )
    capsys.readouterr()
    return out


def test_valid_comparison_passes_both_entry_points(comparison, capsys):
    path = comparison / "comparison.json"
    saved = read_json(path)
    assert read_verified_comparison(comparison) == saved
    for command, flag in (("verify", "--path"),):
        assert main([command, flag, str(comparison)]) == 0
        assert json.loads(capsys.readouterr().out)["status"] == "verified"
    # Whitespace/order do not affect canonical comparison equality; the report
    # remains byte/hash-bound, so this spelling change is only checked directly.
    path.write_text(json.dumps(dict(reversed(list(saved.items()))), indent=2))
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        read_verified_comparison(comparison)


@pytest.mark.parametrize(
    "field,value",
    [
        (("format_version",), 4.0),
        (("schema_version",), 3.0),
        (("sides", 0, "counts", "failed"), False),
        (("sides", 0, "counts", "completed"), 3.0),
        (("sides", 0, "completion_rate", "value"), True),
        (("eligibility", "quality"), 0),
    ],
)
def test_tampered_bool_and_number_rejected_by_dedicated_and_generic_verify(
    comparison, capsys, field, value
):
    path = comparison / "comparison.json"
    saved = read_json(path)
    target = saved
    for key in field[:-1]:
        target = target[key]
    assert target[field[-1]] == value  # Previously Python's equality accepted this swap.
    assert type(target[field[-1]]) is not type(value)
    target[field[-1]] = value
    path.write_bytes(json_bytes(saved))
    with pytest.raises(
        EvidenceError,
        match="comparison_(format_invalid|recomputation_mismatch)|presentation_bytes_changed",
    ):
        read_verified_comparison(comparison)
    for command, flag in (("verify", "--path"),):
        assert main([command, flag, str(comparison)]) == 4
        assert json.loads(capsys.readouterr().out)["status"] == "error"
