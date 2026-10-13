"""Exclusive preparation outputs and fixed, credential-safe failure categories."""

import errno
import hashlib
import json
import os
import platform
import stat
from contextlib import contextmanager
from pathlib import Path

from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms.output_tree import anchored
from inferyard.platforms.platform_io import is_reparse


class PreparationError(RuntimeError):
    def __init__(self, reason, code=2):
        self.reason = reason
        self.code = code
        super().__init__(reason)


def native_platform():
    system, machine = platform.system(), platform.machine().lower()
    architecture = {"amd64": "x64", "x86_64": "x64", "arm64": "arm64", "aarch64": "arm64"}.get(
        machine, machine
    )
    return system, architecture


def require_platform(*allowed):
    current = native_platform()
    if current not in allowed:
        raise PreparationError("unsupported_platform")
    return current


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False) + "\n").encode()


def read_json(path, reason="invalid_input"):
    with Path(path).open("rb") as stream:
        raw = stream.read(16 * 1024 * 1024 + 1)
    if len(raw) > 16 * 1024 * 1024:
        raise PreparationError(reason)
    try:
        return strict_json_loads(raw.decode("utf-8"))
    except ContractError, ValueError, UnicodeError:
        raise PreparationError(reason) from None


def sha256(raw):
    return hashlib.sha256(raw).hexdigest()


def linked(path):
    info = Path(path).lstat()
    return stat.S_ISLNK(info.st_mode) or is_reparse(info)


def asset_identity(path):
    from inferyard.platforms.identity import PreflightError, hash_file

    try:
        return hash_file(Path(path))
    except PreflightError as exc:
        if isinstance(exc.__cause__, OSError):
            raise exc.__cause__ from None
        raise


def file_record(path):

    identity = asset_identity(path)
    return {"path": identity.path, "bytes": identity.size, "sha256": identity.sha256}


def new_path(out, inputs=()):
    """Check the lexical final component before resolving its parent (including dangling links)."""
    out = Path(out).absolute()
    if os.path.lexists(out):
        raise PreparationError("output_exists")
    parent = out.parent.resolve(strict=True)
    if not parent.is_dir():
        raise NotADirectoryError(parent)
    out = parent / out.name
    for source in inputs:
        if source is not None and Path(source).resolve().is_relative_to(out):
            raise PreparationError("invalid_input")
    return out


class NewDirectory:
    """Own one new output tree; never delete or reuse a failed preparation."""

    def __init__(self, path, inputs=()):
        self.path = new_path(path, inputs)
        with self.errors(), anchored(self.path, create=True) as (tree, handle):
            self.identity = tree.identity(handle)

    @staticmethod
    @contextmanager
    def errors():
        try:
            yield
        except OSError as exc:
            if isinstance(exc, FileExistsError) or exc.errno in (errno.ELOOP, errno.ENOTDIR):
                raise PreparationError("output_exists") from None
            raise

    @contextmanager
    def parent(self, relative):
        relative = Path(relative)
        if relative.is_absolute() or ".." in relative.parts:
            raise PreparationError("invalid_input")
        with self.errors(), anchored(self.path, self.identity) as (tree, handle):
            for part in relative.parts:
                handle = tree.descend(handle, part, create=True)
            yield tree, handle

    def check(self):
        with self.parent(Path()):
            pass

    def directory(self, relative):
        with self.parent(relative):
            pass
        return self.path / relative

    @contextmanager
    def stream(self, relative):
        relative = Path(relative)
        if not relative.name or relative.name == "..":
            raise PreparationError("invalid_input")
        with self.parent(relative.parent) as (tree, handle):
            fd = tree.file(handle, relative.name)
            with os.fdopen(fd, "wb") as stream:
                yield stream
                stream.flush()
                os.fsync(stream.fileno())

    def write(self, relative, content):
        with self.stream(relative) as stream:
            stream.write(content)
        return self.path / relative

    def json(self, relative, value):
        return self.write(relative, json_bytes(value))

    def copy(self, relative, source, expected):
        digest, size = hashlib.sha256(), 0
        with self.stream(relative) as stream:
            while chunk := source.read(1024 * 1024):
                size += len(chunk)
                if size > expected["bytes"]:
                    raise PreparationError("runtime_asset_mismatch")
                digest.update(chunk)
                stream.write(chunk)
            if size != expected["bytes"] or digest.hexdigest() != expected["sha256"]:
                raise PreparationError("runtime_asset_mismatch")
        return self.path / relative


def base_details(out):
    return {"out": str(out), "model_requests_sent": 0, "ready_to_run": False}


def finish(output, name, kind, details):
    output.json(name, {"kind": kind, "schema_version": 3, **details})
    return details
