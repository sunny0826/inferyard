"""Export/check regression with a simulated non-UTF-8 Windows text default."""

import importlib.util
import io
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]


@pytest.mark.parametrize("script", ["export_schemas", "export_catalogue"])
@pytest.mark.parametrize("stale", [False, True])
def test_export_and_check_ignore_gbk_default(tmp_path, monkeypatch, script, stale):
    spec = importlib.util.spec_from_file_location(script, ROOT / "scripts" / f"{script}.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    if script == "export_schemas":
        module.__file__ = str(tmp_path / "scripts" / f"{script}.py")
        output = tmp_path / "schemas"
        output.mkdir()
    else:
        output = tmp_path / "data"
        module.OUTPUT = output

    # pathlib resolves an omitted encoding through io.text_encoding. Explicit
    # UTF-8 remains intact; this models cp936 without changing the host locale.
    def gbk_default(encoding, stacklevel=2):
        return "cp936" if encoding is None else encoding

    monkeypatch.setattr(io, "text_encoding", gbk_default)
    control = tmp_path / "encoding-control.txt"
    control.write_bytes("雪".encode())
    with pytest.raises(UnicodeDecodeError):
        control.read_text()
    assert control.read_text(encoding="utf-8") == "雪"

    monkeypatch.setattr(sys, "argv", [f"{script}.py"])
    module.main()
    original = {p: p.read_bytes() for p in output.glob("*.json")}
    assert original
    assert any(any(ord(c) > 127 for c in b.decode("utf-8")) for b in original.values())

    if stale:
        path = next(iter(original))
        path.write_bytes(original[path] + b"\n")
    before = {p: p.read_bytes() for p in original}
    monkeypatch.setattr(sys, "argv", [f"{script}.py", "--check"])
    if stale:
        with pytest.raises(SystemExit, match="stale (schemas|catalogue)"):
            module.main()
    else:
        module.main()
    assert {p: p.read_bytes() for p in original} == before
