"""Published catalogues must describe evidence and remain usable after installation."""

import json
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.errors import ContractError
from inferyard.registry import catalogue


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
