"""Reserved device and control paths must be refused before file I/O."""

import pytest

from scripts.ninfer_source_host.file_io import execute_io
from tests.unit.test_ninfer_source_file_io import Clock, Mem, request


@pytest.mark.parametrize(
    "path",
    [
        "C:\\lab\\COM\u00b9.txt",
        "C:\\lab\\LPT\u00b2",
        "\\\\server\\share\\COM\u00b3.log",
        "C:\\com\u00b9\\note.txt",
        "\\\\lpt\u00b2\\share\\note.txt",
        "C:\\\u0001\\note.txt",
        "C:\\lab\\\u001f.txt",
    ],
    ids=lambda path: path.encode("unicode_escape").decode(),
)
def test_superscript_devices_and_controls_reject_before_io(path):
    api = Mem()
    result = execute_io(request("read", path=path), io_api=api, now_ticks=Clock())
    assert result["primary_error"] == "io_path"
    assert result["bytes"] is result["sha256"] is result["data_b64"] is None
    assert api.calls == []


@pytest.mark.parametrize(
    "path",
    [
        "C:\\lab\\COM10",
        "C:\\lab\\COM\u00b90",
        "C:\\lab\\COM\u2074.txt",
        "C:\\lab\\\u7b14\u8bb0.txt",
    ],
    ids=lambda path: path.encode("unicode_escape").decode(),
)
def test_ordinary_unicode_and_com10_stay_lexical_paths(path):
    api = Mem({path: b"hi"})
    result = execute_io(request("read", path=path), io_api=api, now_ticks=Clock())
    assert result["primary_error"] is None
    assert result["bytes"] == 2
