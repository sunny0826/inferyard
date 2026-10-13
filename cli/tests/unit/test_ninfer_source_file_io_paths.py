"""上标设备名与 U+0001..U+001F。只注入 io_api 和时钟，不打开真实文件。"""

import base64
import json

import pytest

import scripts.ninfer_source_host.file_io as file_io
from scripts.ninfer_source_host.file_io import execute_io, main, validate_io_request
from scripts.ninfer_source_host.file_io_codec import IoError
from tests.unit.test_ninfer_source_file_io import Clock, Mem, request

_REJECTED = (
    "C:\\lab\\COM\u00b9.txt",
    "C:\\lab\\LPT\u00b2",
    "\\\\server\\share\\COM\u00b3.log",
    "C:\\lab\\bad\\\u0001.txt",
    "C:\\com\u00b9\\note.txt",
    "C:\\lab\\LPT\u00b2\\note.txt",
    "C:\\lab\\com\u00b9.TXT",
    "C:\\lab\\Com\u00b2.tar.gz",
    "C:\\lab\\lpt\u00b3.dat",
    "\\\\server\\COM\u00b9\\note.txt",
    "\\\\COM\u00b2\\share\\note.txt",
    "\\\\server\\share\\lpt\u00b3.LOG",
    "\\\\server\\share\\LPT\u00b9\\file.txt",
    "\\\\lpt\u00b2\\share\\note.txt",
    "C:\\\u0001\\note.txt",
    "C:\\lab\\\u001f.txt",
    "C:\\lab\\a\u0007b.txt",
    "\\\\server\\share\\\u0001.log",
    "\\\\server\\\u001f\\note.txt",
    "\\\\\u0001\\share\\note.txt",
    "\\\\server\\share\\bad\\\u0010.txt",
)
_ACCEPTED = (
    "C:\\lab\\COM10",
    "C:\\lab\\com10.txt",
    "C:\\lab\\LPT10",
    "C:\\lab\\COM10\\note.txt",
    "C:\\lab\\COM\u00b90",
    "C:\\lab\\LPT\u00b20.txt",
    "C:\\lab\\XCOM\u00b9.txt",
    "C:\\lab\\COM\u2074.txt",
    "C:\\lab\\\u7b14\u8bb0.txt",
    "C:\\lab\\\u7b2c\u00b9\u7ae0.txt",
    "C:\\lab\\file.NUL",
    "\\\\server\\share\\COM10.log",
    "\\\\server\\share\\\u8bf4\u660e.txt",
)


def _label(path):
    return path.encode("unicode_escape").decode()


@pytest.mark.parametrize("path", _REJECTED, ids=_label)
def test_superscript_devices_and_controls_reject_before_io(path, monkeypatch):
    with pytest.raises(IoError) as caught:
        validate_io_request(request("read", path=path))
    assert caught.value.code == "io_path"

    api = Mem()
    result = execute_io(request("read", path=path), io_api=api, now_ticks=Clock())
    assert result["primary_error"] == "io_path"
    assert result["bytes"] is None
    assert result["sha256"] is None
    assert result["data_b64"] is None
    assert result["cleanup_errors"] == []
    assert api.calls == []

    def refused(*_args, **_kwargs):
        raise AssertionError("executed")

    monkeypatch.setattr(file_io, "execute_io", refused)
    encoded = base64.b64encode(json.dumps(request("read", path=path)).encode()).decode()
    assert main(["worker", encoded]) == 2


@pytest.mark.parametrize("path", _ACCEPTED, ids=_label)
def test_ordinary_unicode_and_com10_stay_lexical_paths(path):
    copied = validate_io_request(request("read", path=path))
    assert copied["path"] == path
