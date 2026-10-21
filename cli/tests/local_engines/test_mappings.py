"""Darwin lsof txt records include mapped data as well as executable images."""

import os
from pathlib import Path

import pytest

from inferyard.platforms.identity import PreflightError
from inferyard.platforms.local_engines import workers

pytestmark = pytest.mark.skipif(os.name != "posix", reason="Darwin mapping identity primitives")


def mapping(path):
    info = path.stat()
    return {
        "f": "txt",
        "t": "REG",
        "n": str(path),
        "D": hex(info.st_dev),
        "i": str(info.st_ino),
    }


def test_mapped_data_does_not_become_a_required_runtime_library(tmp_path, monkeypatch):
    paths = [
        tmp_path / "model.gguf",
        tmp_path / ".plist-cache.fixture",
        tmp_path / "functions.data",
    ]
    for path in paths:
        path.write_bytes(b"GGUF" if path.suffix == ".gguf" else b"mapped data")
    seen = []
    monkeypatch.setattr(workers, "verify_libraries", lambda *a, **kw: seen.append(kw))
    workers.mapped_closure([mapping(p) for p in paths], [], Path("/fixture"))
    assert seen == [{"mapped_paths": []}]


@pytest.mark.parametrize("magic", [b"\xcf\xfa\xed\xfe", b"\xca\xfe\xba\xbf"])
def test_unknown_native_image_rejected_even_with_data_filename(tmp_path, magic):
    path = tmp_path / "functions.data"
    path.write_bytes(magic + b"untrusted native image")
    with pytest.raises(PreflightError, match="mapping_outside_runtime"):
        workers.mapped_closure([mapping(path)], [], Path("/fixture"))


def test_data_classification_rejects_inode_replacement(tmp_path):
    path = tmp_path / "mapped.data"
    path.write_bytes(b"data")
    row = mapping(path)
    replacement = tmp_path / "replacement"
    replacement.write_bytes(b"different data")
    replacement.replace(path)
    with pytest.raises(PreflightError, match="mapping_unverified"):
        workers.mapped_closure([row], [], Path("/fixture"))


def test_data_classification_rejects_fifo_replacement_without_blocking(tmp_path):
    import subprocess
    import sys

    path = tmp_path / "mapped.data"
    path.write_bytes(b"data")
    row = mapping(path)
    path.unlink()
    os.mkfifo(path)
    script = """
import json, sys
from pathlib import Path
from inferyard.platforms.local_engines.workers import mapped_closure
from inferyard.platforms.identity import PreflightError
try:
    mapped_closure([json.loads(sys.argv[1])], [], Path("/fixture"))
except PreflightError as error:
    assert str(error) == "local_engine_mapping_unverified"
else:
    raise AssertionError("FIFO accepted")
"""
    import json

    result = subprocess.run(
        [sys.executable, "-c", script, json.dumps(row)], capture_output=True, text=True, timeout=5
    )
    assert result.returncode == 0, result.stderr


def test_mapping_without_path_is_a_preflight_rejection():
    with pytest.raises(PreflightError, match="mapping_unverified"):
        workers.mapped_closure([{"f": "txt", "t": "REG", "D": "1", "i": "2"}], [], Path("/x"))


def region(path, protection="r--/r--", kind="mapped file"):
    return f"{kind} 100fd0000-100fe4000 [ 80K 80K 0K 0K] {protection} SM=COW {path}"


def test_unlinked_data_requires_live_non_executable_mapping(tmp_path, monkeypatch):
    from inferyard.platforms.local_engines import mapping_permissions

    path = tmp_path / "unlinked.cache"
    path.write_bytes(b"cache")
    row = mapping(path)
    path.unlink()
    monkeypatch.setattr(mapping_permissions, "observe", lambda pid: region(path))
    monkeypatch.setattr(workers, "verify_libraries", lambda *a, **kw: None)
    workers.mapped_closure([row], [], Path("/fixture"), pid=123)
    with pytest.raises(PreflightError, match="mapping_unverified"):
        workers.mapped_closure([row], [], Path("/fixture"))


def test_permission_denied_data_requires_non_executable_mapping(tmp_path, monkeypatch):
    from inferyard.platforms.local_engines import closure, mapping_permissions

    path = tmp_path / "protected.data"
    path.write_bytes(b"protected")
    row = mapping(path)

    def denied(*args, **kwargs):
        raise PermissionError

    monkeypatch.setattr(closure.os, "open", denied)
    monkeypatch.setattr(workers, "verify_libraries", lambda *a, **kw: None)
    monkeypatch.setattr(mapping_permissions, "observe", lambda pid: region(path))
    workers.mapped_closure([row], [], Path("/fixture"), pid=123)
    monkeypatch.setattr(mapping_permissions, "observe", lambda pid: region(path, "r--/r-x"))
    with pytest.raises(PreflightError, match="mapping_unverified"):
        workers.mapped_closure([row], [], Path("/fixture"), pid=123)


@pytest.mark.parametrize(
    "protection,kind",
    [("r-x/r-x", "mapped file"), ("r--/r-x", "mapped file"), ("r--/r--", "__LINKEDIT")],
)
def test_unlinked_code_or_executable_permission_is_not_data(tmp_path, protection, kind):
    from inferyard.platforms.local_engines.mapping_permissions import require_non_executable

    path = tmp_path / "cache"
    with pytest.raises(PreflightError, match="mapping_unverified"):
        require_non_executable(region(path, protection, kind), path)


@pytest.mark.parametrize("text", ["", "malformed /cache", "Process: fixture [123]\n"])
def test_missing_or_malformed_mapping_permissions_are_rejected(text):
    from inferyard.platforms.local_engines.mapping_permissions import require_non_executable

    with pytest.raises(PreflightError, match="mapping_unverified"):
        require_non_executable(text, Path("/cache"))


def test_every_mapping_of_unlinked_path_must_be_non_executable():
    from inferyard.platforms.local_engines.mapping_permissions import require_non_executable

    path = Path("/cache")
    text = region(path) + "\n" + region(path, "r--/r-x")
    with pytest.raises(PreflightError, match="mapping_unverified"):
        require_non_executable(text, path)
