"""The methodology inventory is not a misleading list of supported capabilities."""

import json
from collections import Counter
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.contracts.validation import ContractError
from inferyard.registry import catalogue


def test_all_199_methods_have_distinct_stable_identity_and_source():
    data = catalogue("methods")
    assert len(data["items"]) == 199
    assert len({item["method_id"] for item in data["items"]}) == 199
    assert len({item["category_id"] for item in data["items"]}) == 20
    first = data["items"][0]
    assert first["method_id"] == "method-01-01"
    assert first["title"] == "模型能否成功加载"
    assert first["procedure"] and first["evidence"] and first["applicability"]
    assert len(data["source"]["sha256"]) == 64


def test_metric_inventory_is_complete_but_does_not_claim_planned_algorithms_exist():
    data = catalogue("metrics")
    assert Counter(item["scope"] for item in data["items"]) == {"core": 34, "conditional": 9}
    assert len({item["metric_id"] for item in data["items"]}) == 43
    implemented = {
        item["metric_id"]
        for item in data["items"]
        if item["implementation_status"] == "implemented"
    }
    assert implemented == {
        f"{p}{i:02}"
        for p, n in (("R", 4), ("Q", 8), ("L", 7), ("C", 8), ("S", 4), ("X", 3))
        for i in range(1, n + 1)
    } | {"E01", "E02", "E03", "E04", "E05", "N01", "N02", "N03", "U01"}
    assert all(
        item["implementation_status"] == "planned"
        for item in data["items"]
        if item["metric_id"] not in implemented
    )
    assert not any(item["implementation_status"] == "verified" for item in data["items"])
    metrics = {item["metric_id"]: item for item in data["items"]}
    assert metrics["L06"]["source"] == "verified_engine_timings"
    assert metrics["C04"]["unit"] == "s"


def test_catalogue_cli_is_structured_and_detached(capsys):
    assert main(["catalogue", "--kind", "metrics"]) == 0
    payload = json.loads(capsys.readouterr().out)
    payload["details"]["items"][0]["name"] = "changed"
    assert catalogue("metrics")["items"][0]["name"] != "changed"


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
    from inferyard.adapters.prism import PrismAdapter
    from inferyard.platforms.telemetry import Sampler
    from inferyard.registry import adapter_factory, collector_factory, components

    values = components()["items"]
    identifiers = [v["component_id"] for v in values]
    assert len(identifiers) == len(set(identifiers)) == 23
    assert "svg.phase2.v1" in identifiers
    assert adapter_factory("prism_llama_server_v1") is PrismAdapter
    assert collector_factory("linux-memory.v1") is Sampler
    assert collector_factory("windows-memory.v1") is Sampler
    assert collector_factory("macos-memory.v1") is Sampler
    from inferyard.platforms.resources_macos import ResourceSampler

    assert collector_factory("macos-resource.v1") is ResourceSampler
    from inferyard.platforms.resources_windows import ResourceSampler as WindowsResourceSampler

    assert collector_factory("windows-resource.v1") is WindowsResourceSampler
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
