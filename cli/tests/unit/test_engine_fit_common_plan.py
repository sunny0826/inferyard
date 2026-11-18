"""Frozen single-file assets and engine capabilities never imply model equivalence."""

import struct
from copy import deepcopy

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.config.engine_fit import prepare, validate_plan
from inferyard.contracts.json_digest import digest
from inferyard.platforms.model_asset_manifest import model_asset_manifest
from inferyard.errors import ContractError, PreflightError


def gguf(path, metadata=()):
    content = b"GGUF" + struct.pack("<IQQ", 3, 1, len(metadata))
    for name, kind, value in metadata:
        raw = name.encode()
        content += struct.pack("<Q", len(raw)) + raw + struct.pack("<I", kind)
        content += value
    path.write_bytes(content + b"synthetic bytes; not a loadable model")
    return path


@pytest.fixture
def model(tmp_path, monkeypatch):
    monkeypatch.setattr(
        "inferyard.platforms.engine_fit.host_identity",
        lambda: {"sha256": "a" * 64, "platform": "Darwin"},
    )
    return gguf(tmp_path / "model.gguf")


@pytest.mark.parametrize(
    "metadata",
    [
        [("split.count", 4, struct.pack("<I", 2))],
        [("split.no", 4, struct.pack("<I", 1))],
        [("split.count", 7, b"\x01")],
        [("split.count", 6, struct.pack("<f", 1.0))],
        [("split.count", 9, struct.pack("<IQ", 4, 0))],
    ],
)
def test_split_or_invalid_split_metadata_cannot_be_frozen(tmp_path, metadata):
    model = gguf(tmp_path / "part.gguf", metadata)
    with pytest.raises(PreflightError, match="split_gguf|model_unreadable"):
        model_asset_manifest(model)
