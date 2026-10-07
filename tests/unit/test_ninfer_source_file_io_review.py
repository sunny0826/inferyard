"""Codex 首轮文件审查回归。注入 io_api，不打开真实文件。"""

import base64
import json

import pytest

import scripts.ninfer_source_host.file_io as file_io
from scripts.ninfer_source_host.file_io import execute_io, main, validate_io_request
from scripts.ninfer_source_host.file_io_codec import IoError
from tests.unit.test_ninfer_source_file_io import PATH, Clock, Mem, digest, names, request

_REJECTED = (
    r"C:\lab\CON",
    r"C:\lab\NUL.txt",
    r"C:\lab\note.txt:secret",
    r"C:\CON\note.txt",
    r"C:\lab\aux",
    r"C:\lab\com1.txt",
    r"C:\lab\LPT9.dat",
    r"C:\lab\PRN",
    r"C:\lab\note.txt.",
    r"C:\lab\note.txt ",
    r"C:\lab\a<b.txt",
    r"C:\lab\ask?.txt",
    r"\\CON\share\note.txt",
    r"\\server\NUL\note.txt",
    r"\\server\share\NUL.txt",
    r"\\server\share\note.txt:secret",
    r"\\server\share\note.txt.",
    r"\\server\share \note.txt",
)
_ACCEPTED = (
    r"C:\lab\COM10",
    r"C:\lab\file.NUL",
    r"C:\lab\note.txt",
    r"\\server\share\note.txt",
)


@pytest.mark.parametrize("operation", [[], {}])
def test_non_string_operation_is_rejected_without_io(operation, monkeypatch):
    item = request("read")
    item["operation"] = operation
    with pytest.raises(IoError) as caught:
        validate_io_request(item)
    assert caught.value.code == "io_request"
    api = Mem()
    result = execute_io(item, io_api=api, now_ticks=Clock())
    assert result["primary_error"] == "io_request"
    assert result["bytes"] is None
    assert result["data_b64"] is None
    assert api.calls == []

    def refused(*_args, **_kwargs):
        raise AssertionError("executed")

    monkeypatch.setattr(file_io, "execute_io", refused)
    encoded = base64.b64encode(json.dumps(item).encode()).decode()
    assert main(["worker", encoded]) == 2


@pytest.mark.parametrize("path", _REJECTED)
def test_reserved_ads_and_alias_paths_are_io_path_without_io(path):
    with pytest.raises(IoError) as caught:
        validate_io_request(request("read", path=path))
    assert caught.value.code == "io_path"
    api = Mem()
    result = execute_io(request("read", path=path), io_api=api, now_ticks=Clock())
    assert result["primary_error"] == "io_path"
    assert result["bytes"] is None
    assert result["data_b64"] is None
    assert api.calls == []


@pytest.mark.parametrize("path", _ACCEPTED)
def test_ordinary_names_remain_lexical_paths(path):
    copied = validate_io_request(request("read", path=path))
    assert copied["path"] == path


def test_extra_received_byte_is_counted_without_success_body():
    api = Mem({PATH: b"hix"})
    result = execute_io(request("read"), io_api=api, now_ticks=Clock())
    assert api.handles[1]["off"] == 3
    assert result["bytes"] == 3
    assert result["sha256"] is None
    assert result["data_b64"] is None
    assert result["primary_error"] == "io_failure"
    assert result["sha256"] != digest(b"hi")
    assert result["sha256"] != digest(b"hix")


def test_unknown_extra_does_not_invent_a_count():
    class UnknownExtra(Mem):
        def read(self, handle, size):
            self._hit("read")
            if size == 1:
                return None
            return super().read(handle, size)

    api = UnknownExtra({PATH: b"hi"})
    result = execute_io(request("read"), io_api=api, now_ticks=Clock())
    assert result["bytes"] == 2
    assert result["bytes"] != 0
    assert result["sha256"] is None
    assert result["data_b64"] is None
    assert result["primary_error"] == "io_failure"
    assert names(api).count("close") == 1


def _prepared(clock, monkeypatch, owner, name, replacement):
    api = Mem({PATH: b"hi"}, clock=clock)
    monkeypatch.setattr(owner, name, replacement)
    result = execute_io(request("read"), io_api=api, now_ticks=clock)
    assert result["primary_error"] == "io_deadline"
    assert result["data_b64"] is None
    assert result["bytes"] == 2
    assert result["sha256"] == digest(b"hi")
    assert result["cleanup_errors"] == []
    assert names(api).count("close") == 1
    assert names(api)[-1] == "close"


def test_base64_preparation_past_deadline_is_not_success(monkeypatch):
    clock = Clock()
    encode = file_io.base64.b64encode

    def expire(data):
        clock.t = clock.deadline
        return encode(data)

    _prepared(clock, monkeypatch, file_io.base64, "b64encode", expire)


def test_json_preparation_past_deadline_is_not_success(monkeypatch):
    clock = Clock()
    dump = file_io.json.dumps

    def expire(value, *args, **kwargs):
        clock.t = clock.deadline
        return dump(value, *args, **kwargs)

    _prepared(clock, monkeypatch, file_io.json, "dumps", expire)
