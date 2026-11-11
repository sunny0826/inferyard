"""Published catalogues must describe evidence and remain usable after installation."""

import json
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.errors import ContractError
from inferyard.registry import catalogue


def test_metric_inventory_is_complete_but_does_not_claim_planned_algorithms_exist():
    data = catalogue("metrics")
    assert not any(item["implementation_status"] == "verified" for item in data["items"])
    metrics = {item["metric_id"]: item for item in data["items"]}
    assert metrics["L06"]["source"] == "verified_engine_timings"
    assert metrics["C04"]["unit"] == "s"
    # Engine comparison metrics take new IDs; existing IDs are never reassigned.
    for key in ("L08", "L09"):
        assert metrics[key]["unit"] == "ms"
        assert metrics[key]["implementation_status"] == "implemented"
    assert len(metrics) == 45


def test_packaged_metric_changes_are_part_of_tool_identity(tmp_path, monkeypatch):
    from inferyard import provenance

    monkeypatch.setattr(provenance, "__file__", str(tmp_path / "provenance.py"))
    definition = tmp_path / "metrics.json"
    definition.write_text('{"window":"v1"}')
    before = provenance.tool_source_hash()
    definition.write_text('{"window":"v2"}')
    assert provenance.tool_source_hash() != before
    unchanged = provenance.tool_source_hash()
    (tmp_path / "cache.pyc").write_bytes(b"cache")
    assert provenance.tool_source_hash() == unchanged


def test_component_registry_routes_only_implemented_components():
    from inferyard.registry import adapter_factory, collector_factory

    for lookup in (adapter_factory, collector_factory):
        with pytest.raises(ContractError, match="not implemented"):
            lookup("unimplemented")


@pytest.mark.parametrize("kind", ["methods", "metrics"])
def test_catalogue_cli_preserves_unicode_with_cp1252_default(monkeypatch, capsys, kind):
    expected = catalogue(kind)
    read_text = Path.read_text

    def legacy_read(path, encoding=None, **kwargs):
        return read_text(path, encoding=encoding or "cp1252", **kwargs)

    monkeypatch.setattr(Path, "read_text", legacy_read)
    assert main(["catalogue", "--kind", kind]) == 0
    captured = capsys.readouterr()
    assert not captured.err
    assert json.loads(captured.out)["details"] == expected
