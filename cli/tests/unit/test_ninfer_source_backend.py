"""Windows Backend 注入回归。fake API 不构成原生 Job、管道或句柄证据。"""

import pytest

from scripts.ninfer_source_host.windows_backend import WindowsBackend

SHA = "ab" * 32
OTHER = "cd" * 32


class HostError(Exception):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


class BackendError(HostError):
    def __init__(self, code="host_backend"):
        super().__init__(code)


class BackendStartError(BackendError):
    def __init__(self, ref, cause):
        super().__init__()
        self.ref = ref
        self.cause = cause


ERRS = (HostError, BackendError, BackendStartError)


class Pipe:
    def __init__(self, data=b"", eof=False):
        self.data = data
        self.eof = eof


class FakeApi:
    def __init__(self, fail=None):
        self.fail = fail
        self.order = []
        self.process = object()
        self.thread = object()
        self.stdout = Pipe()
        self.stderr = Pipe()
        self.job = object()
        self.flags = None
        self.active = None
        self.creation = None
        self.assigned = None
        self.resumed = None
        self.read_sizes = []
        self.peeked = []
        self.wait_result = 0x102
        self.wait_raises = False
        self.exit_value = 0
        self.exit_raises = False
        self.exit_process = None
        self.waited = None
        self.closed = []
        self.close_raises = False
        self.terminated = []
        self.killed = []

    def spawn_suspended(self, argv, cwd, creation_flags):
        self.order.append("spawn")
        self.creation = creation_flags
        self.argv = argv
        self.cwd = cwd
        if self.fail == "spawn":
            raise OSError("spawn")
        if self.fail == "partial":
            error = OSError("partial")
            error.spawned = Pipe()
            error.spawned.process = self.process
            error.spawned.thread = self.thread
            error.spawned.stdout = self.stdout
            error.spawned.stderr = self.stderr
            error.extra = (object(),)
            raise error
        return _spawned(self)

    def create_job(self, flags, active_limit):
        self.order.append("job")
        self.flags = flags
        self.active = active_limit
        if self.fail == "job":
            raise OSError("job")
        if self.fail == "job-leak":
            error = OSError("job")
            error.leaked_job = self.job
            raise error
        return self.job

    def assign_job(self, job, process):
        self.order.append("assign")
        self.assigned = (job, process)
        if self.fail == "assign":
            raise OSError("assign")

    def resume(self, thread):
        self.order.append("resume")
        self.resumed = thread
        if self.fail == "resume":
            raise OSError("resume")

    def peek(self, pipe):
        self.peeked.append(pipe)
        return len(pipe.data), pipe.eof

    def read(self, pipe, size):
        self.read_sizes.append(size)
        chunk = pipe.data[:size]
        pipe.data = pipe.data[size:]
        return chunk

    def wait_zero(self, process):
        self.waited = process
        if self.wait_raises:
            raise OSError("wait")
        return self.wait_result

    def get_exit_code(self, process):
        self.exit_process = process
        if self.exit_raises:
            raise OSError("exit")
        return self.exit_value

    def terminate(self, process):
        self.terminated.append(process)

    def kill(self, process):
        self.killed.append(process)

    def close(self, handle):
        self.closed.append(handle)
        if self.close_raises:
            raise OSError("close")


def _spawned(api):
    spawned = Pipe()
    spawned.process = api.process
    spawned.thread = api.thread
    spawned.stdout = api.stdout
    spawned.stderr = api.stderr
    return spawned


def spec(**over):
    base = {
        "argv": ("C:\\lab\\tool.exe", "--once"),
        "cwd": "C:\\lab",
        "executable_sha256": SHA,
        "script_sha256": OTHER,
        "no_descendants": True,
    }
    base.update(over)
    return base


def backend(fail=None, *, errors=ERRS):
    api = FakeApi(fail)
    return api, WindowsBackend(api, errors=errors)


@pytest.mark.parametrize(
    "over",
    [
        {"argv": ["C:\\lab\\tool.exe"]},
        {"argv": ()},
        {"argv": ("bad\0",)},
        {"argv": ("\ud800",)},
        {"argv": ("x" * 12001,)},
        {"cwd": "relative"},
        {"cwd": "C:relative"},
        {"cwd": "\\\\?\\C:\\lab"},
        {"cwd": "C:\\lab\\..\\x"},
        {"executable_sha256": "AB" * 32},
        {"executable_sha256": SHA + "\n"},
        {"no_descendants": False},
        {"no_descendants": 1},
        {"extra": 1},
    ],
)
def test_bad_spec_does_nothing(over):
    api, host = backend()
    with pytest.raises(HostError) as caught:
        host.start(spec(**over))
    assert caught.value.code == "host_job"
    assert api.order == []
    assert api.terminated == []
    assert api.closed == []
