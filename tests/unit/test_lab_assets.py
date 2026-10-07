"""Native-v3 framing and component-ledger errors use bounded synthetic local files."""

import hashlib
import json
import struct
from copy import deepcopy

import pytest

from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError, static_preflight
from inferyard.platforms.lab_assets import inspect_asset
from inferyard.platforms.lab_identity import LabFileIdentity
from inferyard.platforms.windows_lab_api import override_windows_lab_api
from tests.unit.test_windows_lab_files import STAMP, Store


@pytest.fixture
def container(tmp_path):
    directory = {
        "components": {"text": {"config": {"model_type": "fixture"}}},
        "objects": [{"id": "one", "offset": 0, "bytes": 4}],
        "bindings": {},
        "uses": [],
        "files": [{"path": None, "payload_bytes": 4}],
    }
    artifact_id = "1" * 32
    model_path, ledger_path = tmp_path / "model.ninfer", tmp_path / "ledger.json"
    model = {
        "kind": "ninfer",
        "local_path": str(model_path),
        "bytes": 4100,
        "sha256": "a" * 64,
        "component_ledger_path": str(ledger_path),
    }

    def write(data=directory, magic=b"NINFER\0\x03", ledger_change=None):
        raw = json_bytes(data)
        header = magic + struct.pack("<Q", len(raw)) + bytes.fromhex(artifact_id)
        model_path.write_bytes(header + raw + bytes(4096 - len(header) - len(raw)) + b"data")
        ledger = {
            "artifact_id": artifact_id,
            "model_sha256": model["sha256"],
            "components_sha256": hashlib.sha256(json_bytes(data["components"])).hexdigest(),
        }
        ledger.update(ledger_change or {})
        ledger_path.write_text(json.dumps(ledger), encoding="utf-8")

    write()
    return {"model": model}, directory, write


def test_native_header_and_ledger_bind_exact_components(container):
    config, _, _ = container
    assert inspect_asset(config)["artifact_id"] == "1" * 32


@pytest.mark.parametrize(
    "change,reason",
    [
        ("magic", "header_invalid"),
        ("part", "split_model"),
        ("overlap", "objects_invalid"),
        ("out_of_bounds", "objects_invalid"),
        ("ledger", "ledger_mismatch"),
        ("size", "directory_bounds"),
    ],
)
def test_native_asset_refusal_paths(container, change, reason):
    config, directory, write = container
    bad = deepcopy(directory)
    kwargs = {}
    if change == "magic":
        kwargs["magic"] = b"NINFER\0\x02"
    elif change == "part":
        bad["files"].append({"path": "model.part-0001", "payload_bytes": 4})
    elif change == "overlap":
        bad["objects"].append({"id": "two", "offset": 0, "bytes": 1})
    elif change == "out_of_bounds":
        bad["objects"][0]["bytes"] = 5
    elif change == "ledger":
        kwargs["ledger_change"] = {"artifact_id": "2" * 32}
    elif change == "size":
        config["model"]["bytes"] -= 1
    write(bad, **kwargs)
    with pytest.raises(PreflightError, match=reason):
        inspect_asset(config)


def test_file_guard_uses_native_identity_and_refuses_reparse():
    store = Store()
    volume, identifier, attributes, size, creation, write, change = STAMP
    identity = LabFileIdentity(
        r"C:\lab\model.gguf",
        "a" * 64,
        size,
        volume,
        int.from_bytes(identifier, "big"),
        write * 100,
        [volume, identifier.hex(), attributes, size, creation, write, change],
        "model",
    )
    with override_windows_lab_api(store):
        assert identity.unchanged()
        store.change_path_at = store.path_reads + 1
        assert not identity.unchanged()
        store.change_path_at = None
        store.reparse.add(r"C:\lab")
        assert not identity.unchanged()


def test_real_preflight_blocks_lab_engine_on_mac_before_any_asset_read(monkeypatch):
    monkeypatch.setattr("inferyard.platforms.identity.platform.system", lambda: "Darwin")
    with pytest.raises(PreflightError, match="lab_adapter_requires_windows"):
        static_preflight(
            {
                "engine": {"adapter": "ninfer", "backend": "cpu"},
                "endpoint": {"url": "http://127.0.0.1:8080"},
            }
        )
