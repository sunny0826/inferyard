import hashlib
import os
from pathlib import Path

import pytest

from inferyard.errors import PreflightError, UnsupportedFormat
from inferyard.contracts.validation import json_bytes
from inferyard.platforms import engine_fit as fit


def _process(proc, pid, *, parent=0, ticks=67890, rss=2, children=()):
    directory = proc / str(pid)
    directory.mkdir(exist_ok=True)
    fields = ["S"] + ["0"] * 23
    for index, value in ((1, parent), (11, 50), (12, 25), (19, ticks), (21, rss)):
        fields[index] = str(value)
    (directory / "stat").write_text(f"{pid} (server with ) spaces) " + " ".join(fields))
    task = directory / "task" / str(pid)
    task.mkdir(parents=True, exist_ok=True)
    (task / "children").write_text(" ".join(map(str, children)))
    return directory


def _cmdline(directory, *args):
    (directory / "cmdline").write_bytes(b"\0".join(os.fsencode(arg) for arg in (*args, "")))


@pytest.fixture
def service(tmp_path):
    if os.name == "nt":
        pytest.skip("Linux /proc fixtures require POSIX symlinks and uid")
    proc = tmp_path / "proc"
    proc.mkdir()
    (proc / "meminfo").write_text("MemAvailable: 2048 kB\n")
    directory = _process(proc, 123)
    (directory / "fd").mkdir()
    (directory / "net").mkdir()
    model = tmp_path / "model"
    model.mkdir()
    (model / "weights").write_bytes(b"model")
    binary = tmp_path / "python3"
    binary.write_bytes(b"python executable fixture")
    (directory / "exe").symlink_to(binary)
    (directory / "cwd").symlink_to(tmp_path)
    _cmdline(directory, "python3", "-m", "vllm.entrypoints.openai.api_server", "--model", model)
    (directory / "fd/5").symlink_to("socket:[456]")
    (directory / "net/tcp").write_text(
        "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"
        "0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 456\n"
    )
    return proc, directory, model


@pytest.mark.parametrize("change", ["pid", "argv", "binary", "listener", "model_inode", "cwd"])
def test_check_service_detects_identity_changes(service, change):
    proc, directory, model = service
    _cmdline(directory, "vllm", "serve", "model")
    binding = fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    if change == "pid":
        _process(proc, 123, ticks=67891)
    elif change == "argv":
        _cmdline(directory, "vllm", "serve", "model", "--dtype", "float32")
    elif change == "binary":
        (directory / "exe").write_bytes(b"changed interpreter")
    elif change == "listener":
        (directory / "fd/5").unlink()
    elif change == "model_inode":
        model.rename(model.with_name("old-model"))
        model.mkdir()
    else:
        (directory / "cwd").unlink()
        (directory / "cwd").symlink_to(model)
    with pytest.raises(PreflightError):
        fit.check_service(binding, proc_root=proc)


@pytest.mark.parametrize("change", ["root_reused", "child_missing", "child_reparented", "bad_rss"])
def test_unreadable_or_changed_process_tree_is_missing_not_partial(service, change):
    proc, _, model = service
    binding = fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    if change == "root_reused":
        _process(proc, 123, ticks=1)
    elif change == "bad_rss":
        _process(proc, 123, rss=-1)
    else:
        _process(proc, 123, children=[456])
        if change == "child_reparented":
            _process(proc, 456, parent=999)
    snapshot = fit.resource_snapshot(binding, proc_root=proc)
    for field in ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count"):
        assert snapshot[field] is None
        assert snapshot["missing_reasons"][field]
    assert snapshot["memory_available_bytes"] == 2048 * 1024
