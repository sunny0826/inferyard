"""Mac rejection tests for deadline-aware Windows file manifests."""

import time
from pathlib import Path

import pytest

from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import PreflightError
from inferyard.platforms import windows_lab_checks as checks
from inferyard.platforms import windows_lab_files as files
from inferyard.platforms.windows_lab_api import override_windows_lab_api

ROOT = Path(__file__).parents[1] / "fixtures" / "windows_lab"
CONTENT = b"model-bytes"
STAMP = (2**64 - 1, bytes(range(16)), 32, len(CONTENT), 100, 200, 300)


class Store:
    def __init__(self):
        self.content = CONTENT
        self.stamp = STAMP
        self.reparse = set()
        self.checked = []
        self.closed = []
        self.path_reads = 0
        self.fd_reads = 0
        self.change_path_at = None
        self.change_fd_at = None
        self.offset = 0
        self.reads = 0
        self.clock = None
        self.expire_after_read = None

    def reject_reparse(self, path):
        self.checked.append(path)
        if path in self.reparse:
            raise OSError("reparse_point_rejected")

    def path_stamp(self, path):
        self.path_reads += 1
        return self._changed(self.path_reads, self.change_path_at)

    def open_read(self, path):
        self.handle = object()
        self.offset = 0
        return self.handle

    def descriptor_stamp(self, handle):
        assert handle is self.handle
        self.fd_reads += 1
        return self._changed(self.fd_reads, self.change_fd_at)

    def read_file(self, handle, size):
        assert handle is self.handle
        self.reads += 1
        if self.expire_after_read is not None and self.reads >= self.expire_after_read:
            self.clock["expire"] = True
        chunk = self.content[self.offset : self.offset + size]
        self.offset += len(chunk)
        return chunk

    def close_file(self, handle):
        if handle in self.closed:
            raise AssertionError("double close")
        self.closed.append(handle)

    def _changed(self, reads, at):
        if at is not None and reads >= at:
            return (self.stamp[0], b"\xff" + self.stamp[1][1:], *self.stamp[2:])
        return self.stamp
