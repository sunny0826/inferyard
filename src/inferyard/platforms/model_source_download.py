"""Child-owned streaming transfer, durability and exclusive atomic publication."""

import ctypes
import errno
import hashlib
import os
import shutil
import sys
import time
from pathlib import Path

import httpx

from inferyard.config.preparation_io import NewDirectory, PreparationError, new_path
from inferyard.platforms.model_source import MISMATCH, parse_metadata, validate_token_env
from inferyard.platforms.model_source_http import INCOMPLETE, metadata_bytes, remaining, response
from inferyard.platforms.platform_io import sync_directory

DISK_OVERHEAD = 1024 * 1024
CHUNK_SIZE = 1024 * 1024


def publish(output, temporary, final):
    """Rename without replacement, using held directory handles on POSIX."""
    with output.parent(Path()) as (_, handle):
        if os.name == "nt":
            from inferyard.platforms.windows_api import move_file

            move_file(output.path / temporary, output.path / final, False)
        else:
            library = ctypes.CDLL(None, use_errno=True)
            name, flag = ("renameatx_np", 4) if sys.platform == "darwin" else ("renameat2", 1)
            rename = getattr(library, name, None)
            if rename is None:
                raise OSError(errno.ENOSYS, "exclusive rename unavailable")
            rename.argtypes = [
                ctypes.c_int,
                ctypes.c_char_p,
                ctypes.c_int,
                ctypes.c_char_p,
                ctypes.c_uint,
            ]
            rename.restype = ctypes.c_int
            if rename(handle, os.fsencode(temporary), handle, os.fsencode(final), flag):
                code = ctypes.get_errno()
                raise OSError(code, "exclusive rename failed")
            os.fsync(handle)
    sync_directory(output.path)


def _remove(output, name):
    with output.parent(Path()) as (_, handle):
        try:
            if os.name == "nt":
                (output.path / name).unlink()
            else:
                os.unlink(name, dir_fd=handle)
        except FileNotFoundError:
            pass


def _token(name):
    validate_token_env(name)
    if name is None:
        return None
    value = os.environ.get(name)
    if not value or not value.isascii() or any(ord(c) < 33 or ord(c) == 127 for c in value):
        raise PreparationError(INCOMPLETE)
    return value


def _transfer(client, source, out, token, budget, deadline):
    record = parse_metadata(source, metadata_bytes(client, source, token, budget, deadline))
    if record["bytes"] > budget.max_bytes:
        raise PreparationError(INCOMPLETE)
    if record["bytes"] + DISK_OVERHEAD > shutil.disk_usage(out.parent).free:
        raise OSError(errno.ENOSPC, "insufficient model storage")
    remaining(deadline)
    output = NewDirectory(out)
    name = source.path.rsplit("/", 1)[-1]
    temporary = name + ".part"
    digest, size = hashlib.sha256(), 0
    try:
        with response(client, source, source.download_url, token, budget, deadline) as opened:
            length = opened.headers.get("content-length")
            if length is not None and (not length.isdecimal() or int(length) != record["bytes"]):
                raise PreparationError(MISMATCH)
            with output.stream(temporary) as stream:
                for chunk in opened.iter_raw(chunk_size=CHUNK_SIZE):
                    remaining(deadline)
                    size += len(chunk)
                    if size > record["bytes"] or size > budget.max_bytes:
                        raise PreparationError(MISMATCH)
                    if stream.write(chunk) != len(chunk):
                        raise OSError(errno.EIO, "short model write")
                    digest.update(chunk)
                if size != record["bytes"] or digest.hexdigest() != record["sha256"]:
                    raise PreparationError(MISMATCH)
        remaining(deadline)
        publish(output, temporary, name)
        output.json("source.json.part", record)
        remaining(deadline)
        publish(output, "source.json.part", "source.json")
    except BaseException:
        _remove(output, temporary)
        _remove(output, "source.json.part")
        raise
    return {
        "out": str(output.path),
        "model": str(output.path / name),
        "source_record": str(output.path / "source.json"),
        "bytes": size,
        "sha256": digest.hexdigest(),
        "ready_to_run": False,
        "model_requests_sent": 0,
    }


def transfer(source, out, token_env, budget, *, client=None):
    """Production callers execute this only in the owned direct child."""
    deadline = time.monotonic() + budget.total_seconds
    try:
        out = new_path(out)
        token = _token(token_env)
        if client is not None:
            return _transfer(client, source, out, token, budget, deadline)
        with httpx.Client(trust_env=False, follow_redirects=False) as owned:
            return _transfer(owned, source, out, token, budget, deadline)
    except OSError:
        raise PreparationError("io_error", 4) from None
