"""Verify packaged community resource loading and path-traversal rejection."""

import json

import pytest

from inferyard.provisioning.community_resources import resource_bytes
from inferyard.provisioning.preparation_io import PreparationError


@pytest.mark.parametrize(
    "bad_name",
    [
        "",
        ".",
        "..",
        "../methods.json",
        "bundles/../methods.json",
        "/etc/passwd",
        "bundles\\zh-smoke.json",
        "bundles/zh-smoke.json\x00",
        "C:bundles/zh-smoke.json",
        "bundles/missing.json",
    ],
)
def test_community_resource_bytes_rejects_invalid_paths(bad_name: str) -> None:
    with pytest.raises(PreparationError):
        resource_bytes(bad_name)


def test_community_resource_bytes_reads_packaged_bundle() -> None:
    raw = resource_bytes("bundles/zh-smoke.json")
    data = json.loads(raw.decode("utf-8"))
    assert data["bundle_id"] == "zh-smoke-v1"
