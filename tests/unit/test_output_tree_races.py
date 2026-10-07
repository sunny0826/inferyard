"""Deterministic output races use only disposable temporary directories."""

import io
import os

import pytest

from inferyard.config.preparation_io import NewDirectory, PreparationError, sha256
from inferyard.platforms import output_tree


@pytest.mark.skipif(os.name == "nt", reason="POSIX descriptor race injection")
@pytest.mark.parametrize("location", ["ancestor", "root", "nested"])
@pytest.mark.parametrize("operation", ["write", "copy", "json", "mkdir"])
def test_swapped_directory_never_writes_external_target(tmp_path, monkeypatch, location, operation):
    base, outside = tmp_path / "base", tmp_path / "outside"
    base.mkdir()
    outside.mkdir()
    output = NewDirectory(base / "new")
    output.directory("assets")
    victim = {"ancestor": base, "root": output.path, "nested": output.path / "assets"}[location]
    old = tmp_path / "held-original"
    fired = False

    def swap():
        nonlocal fired
        if not fired:
            fired = True
            victim.rename(old)
            victim.symlink_to(outside, target_is_directory=True)

    if operation == "mkdir":
        original = os.mkdir

        def mkdir(name, *args, **kwargs):
            if name == "child":
                swap()
            return original(name, *args, **kwargs)

        monkeypatch.setattr(output_tree.os, "mkdir", mkdir)
    else:
        original = output_tree.PosixTree.file

        def file(self, parent, name):
            swap()
            return original(self, parent, name)

        monkeypatch.setattr(output_tree.PosixTree, "file", file)
    with pytest.raises(PreparationError, match="output_exists"):
        if operation == "mkdir":
            output.directory("assets/child")
        elif operation == "copy":
            output.copy(
                "assets/file", io.BytesIO(b"probe"), {"bytes": 5, "sha256": sha256(b"probe")}
            )
        elif operation == "json":
            output.json("assets/file", {"probe": True})
        else:
            output.write("assets/file", b"probe")
    assert fired
    assert list(outside.rglob("*")) == []
    assert old.exists()  # Preserve failed output; never delete another actor's replacement.


@pytest.mark.skipif(os.name == "nt", reason="POSIX fd accounting")
@pytest.mark.parametrize("failure", [False, True])
def test_directory_handles_close_on_success_and_exception(tmp_path, monkeypatch, failure):
    opened, closed = [], []
    original_open, original_close = os.open, os.close

    def open_fd(*args, **kwargs):
        fd = original_open(*args, **kwargs)
        if args[1] & os.O_DIRECTORY:
            opened.append(fd)
        return fd

    def close_fd(fd):
        closed.append(fd)
        return original_close(fd)

    monkeypatch.setattr(output_tree.os, "open", open_fd)
    monkeypatch.setattr(output_tree.os, "close", close_fd)
    output = NewDirectory(tmp_path / "new")
    if failure:

        class BrokenSource:
            def read(self, size):
                raise OSError("synthetic read failure")

        with pytest.raises(OSError, match="synthetic"):
            output.copy("assets/file", BrokenSource(), {"bytes": 1, "sha256": "0" * 64})
    else:
        output.write("assets/file", b"x")
    assert sorted(opened) == sorted(closed)


@pytest.mark.skipif(os.name != "nt", reason="requires native Windows sharing semantics")
@pytest.mark.parametrize("location", ["ancestor", "root", "nested"])
def test_windows_held_directories_cannot_be_replaced(tmp_path, monkeypatch, location):
    from inferyard.platforms.output_tree_windows import WindowsTree

    base = tmp_path / "base"
    base.mkdir()
    output = NewDirectory(base / "new")
    output.directory("assets")
    victim = {"ancestor": base, "root": output.path, "nested": output.path / "assets"}[location]
    original = WindowsTree.file
    attempted = []

    def file(self, parent, name):
        with pytest.raises(OSError):
            victim.rename(tmp_path / "moved")
        attempted.append(True)
        return original(self, parent, name)

    monkeypatch.setattr(WindowsTree, "file", file)
    output.write("assets/file", b"probe")
    assert attempted and (output.path / "assets/file").read_bytes() == b"probe"
    victim.rename(tmp_path / "moved")  # Handles were released after the operation.
