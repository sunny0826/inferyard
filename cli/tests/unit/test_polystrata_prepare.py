"""Unit tests for PolyStrata archive traversal and insecure source rejection."""

from __future__ import annotations

from pathlib import Path

import pytest

from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB
from inferyard.provisioning.preparation_io import PreparationError
from polystrata_test_helpers import _disk_usage, _tar_gz_file


def test_archive_rejects_unsafe_entries_and_invalid_sources(tmp_path: Path):
    evil_tar = _tar_gz_file(
        tmp_path / "evil.tar.gz",
        [
            ("file", "setup.py", b"print(1)\n"),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
            ("symlink", "escape", "../../etc/passwd"),
        ],
    )
    with pytest.raises(PreparationError) as exc_info:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-evil",
            data_dir=tmp_path / "data-evil",
            source=str(evil_tar),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_info.value.reason == "unsafe_archive_entry"

    with pytest.raises(PreparationError) as exc_http:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-http",
            source="http://example.test/polystrata.tar.gz",
        )
    assert exc_http.value.reason == "invalid_engine_source"
