"""Lightweight local installation checks: package mutations in runtime_roots do not block."""

import os

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms.local_engines import installation as runtime


@pytest.fixture
def installation(tmp_path, monkeypatch):
    monkeypatch.setattr(runtime, "darwin", lambda: None)
    root = tmp_path / "runtime"
    root.mkdir()
    executable = root / "server"
    executable.write_bytes(b"synthetic executable")
    (root / "package.py").write_text("value = 1\n")
    value = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": "llama-cpp",
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "runtime_roots": [str(root)],
        "environment": {},
    }
    return value, root


@pytest.mark.parametrize("mutation", ["modify", "add", "delete", "pyc", "symlink", "same_mtime"])
def test_runtime_roots_mutations_are_ignored(installation, mutation):
    declaration, root = installation
    snapshot = runtime.freeze_installation(declaration)
    source = root / "package.py"
    if mutation == "modify":
        source.write_text("different\n")
    elif mutation == "add":
        (root / "injected.py").write_text("injected\n")
    elif mutation == "delete":
        source.unlink()
    elif mutation == "pyc":
        (root / "package.pyc").write_bytes(b"bytecode")
    elif mutation == "symlink":
        source.unlink()
        source.symlink_to(root / "server")
    else:
        before = source.stat()
        source.write_text("value = 2\n")
        os.utime(source, ns=(before.st_atime_ns, before.st_mtime_ns))
    identities = runtime.verify_installation(snapshot)
    assert len(identities) == 1
    assert identities[0].path == str((root / "server").resolve())


def test_missing_executable_still_rejected(installation):
    declaration, root = installation
    snapshot = runtime.freeze_installation(declaration)
    (root / "server").unlink()
    with pytest.raises(PreflightError, match="local_engine_locator_missing"):
        runtime.verify_installation(snapshot)
