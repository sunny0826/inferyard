"""Portable filenames and manifest-bound resolution of older body snapshots."""

import re
from ntpath import isreserved

import pytest

from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.request_snapshots import (
    require_current_sources,
    snapshot_filename,
    snapshot_name_for_event,
)
from inferyard.evidence.storage import (
    EvidenceError,
    EvidenceStore,
    json_bytes,
    local_file,
    read_json,
)


def test_portable_names_sort_in_request_order_and_publish_normally(tmp_path):
    store = EvidenceStore(tmp_path, run_id="20261001T032200Z-abcd")
    names = []
    try:
        for ordinal, phase in ((1, "probe"), (2, "probe"), (9, "warmup"), (10, "formal")):
            name = snapshot_filename(store.run_id, phase, ordinal)
            assert re.fullmatch(r"[A-Za-z0-9._-]+", name) and not isreserved(name)
            assert not name.endswith((" ", "."))
            store.snapshot(name, {"ordinal": ordinal, "phase": phase})
            assert read_json(store.path / name) == {"ordinal": ordinal, "phase": phase}
            names.append(name)
        assert sorted(names) == names
        assert names[-1] == "20261001T032200Z-abcd__000010__formal.request.json"
    finally:
        store.close()


@pytest.mark.parametrize(
    "run_id",
    [
        "bad:name",
        "../run",
        "bad\\run",
        "bad run",
        "机型",
        "NUL.foo",
        "COM1.log",
        "run\x00",
        "",
        "a" * 255,
    ],
)
def test_unsafe_or_overlong_names_fail_before_publication(run_id):
    with pytest.raises(EvidenceError):
        snapshot_filename(run_id, "formal", 1)


@pytest.mark.parametrize("ordinal", [0, -1, True, 1.5, "1"])
def test_invalid_ordinal_is_not_silently_coerced(ordinal):
    with pytest.raises(EvidenceError):
        snapshot_filename("run", "formal", ordinal)


def event(request_id):
    return {"run_id": "run-old", "phase": "formal", "request_id": request_id}


@pytest.mark.parametrize("request_id", ["formal-12", "run-old-formal-12", "run-old:formal:12"])
def test_legacy_filenames_are_explicitly_rejected(tmp_path, request_id):
    original = request_id + ".request.json"
    # The name can be checked from a manifest even on Windows, without opening an ADS.
    with pytest.raises(UnsupportedFormat):
        require_current_sources(tmp_path, {original: {}})


@pytest.mark.parametrize("request_id", ["run-old-formal-12", "run-old:formal:12"])
def test_current_filename_resolution_does_not_change_event_identity(tmp_path, request_id):
    record = event(request_id)
    name = snapshot_filename("run-old", "formal", 12)
    (tmp_path / name).write_bytes(json_bytes({"body": "canonical"}))
    assert snapshot_name_for_event(tmp_path, record) == name
    assert snapshot_name_for_event(tmp_path, record, {name: {}}) == name
    assert record["request_id"] == request_id


def test_opaque_request_id_cannot_escape_root_or_gain_a_snapshot(tmp_path):
    resolved = snapshot_name_for_event(tmp_path, event("../outside"))
    assert local_file(tmp_path, resolved).parent == tmp_path
    assert not (tmp_path / resolved).exists()
