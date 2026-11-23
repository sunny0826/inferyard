"""Resumable transfer helpers and NDJSON stderr progress emission."""

from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import sys
import time
from typing import TextIO

from inferyard.platforms.output_tree import anchored
from inferyard.platforms.platform_io import open_nofollow, sync_directory
from inferyard.provisioning.acquire_errors import classify_http_status, failure_details
from inferyard.provisioning.preparation_io import NewDirectory, PreparationError, json_bytes, linked

_CONTENT_RANGE_RE = re.compile(r"^bytes\s+(\d+)-(\d+)/(\d+|\*)$", re.IGNORECASE)
_UNSATISFIED_RANGE_RE = re.compile(r"^bytes\s+\*/(\d+)$", re.IGNORECASE)


def _header_value(headers, name: str) -> str | None:
    if headers is None:
        return None
    raw = headers.get(name)
    if raw is None and hasattr(headers, "items"):
        target = name.lower()
        for key, value in headers.items():
            if isinstance(key, str) and key.lower() == target:
                raw = value
                break
    return None if raw is None else str(raw).strip()


def parse_content_length(headers) -> int | None:
    """Safely parse Content-Length into a positive int or None."""
    text = _header_value(headers, "content-length")
    if not text or not text.isdigit():
        return None
    value = int(text)
    return value if value > 0 else None


def parse_content_range(headers, expected_offset: int) -> tuple[bool, int | None]:
    """Validate Content-Range for a 206 response starting at expected_offset."""
    text = _header_value(headers, "content-range")
    if not text:
        return False, None
    match = _CONTENT_RANGE_RE.fullmatch(text)
    if match is None:
        return False, None
    start = int(match.group(1))
    end = int(match.group(2))
    raw_total = match.group(3)
    total = None if raw_total == "*" else int(raw_total)
    if start != expected_offset or end < start:
        return False, None
    if total is not None and total <= end:
        return False, None
    return True, total


def parse_unsatisfied_range_total(headers) -> int | None:
    """Parse Content-Range total from a 416 Range Not Satisfiable response."""
    text = _header_value(headers, "content-range")
    if not text:
        return None
    match = _UNSATISFIED_RANGE_RE.fullmatch(text)
    if match is None:
        return None
    total = int(match.group(1))
    return total if total > 0 else None


class ProgressEmitter:
    """Emit throttled NDJSON transfer.progress events to stderr without URLs or secrets."""

    def __init__(
        self,
        enabled: bool = False,
        stream: TextIO | None = None,
        min_interval_seconds: float = 0.25,
    ) -> None:
        self.enabled = bool(enabled)
        self._stream = stream
        self.min_interval_seconds = max(0.0, float(min_interval_seconds))
        self._last_stage: str | None = None
        self._last_step: int | None = None
        self._last_emit_at: float | None = None
        self._window: list[tuple[float, int]] = []

    def _compute_rate(self, stage: str, downloaded: int, now: float, transition: bool) -> float | None:
        if stage not in ("downloading", "downloading_runtime", "downloading_model"):
            self._window.clear()
            return None
        if transition or not self._window or downloaded < self._window[-1][1]:
            self._window = [(now, downloaded)]
            return None
        self._window.append((now, downloaded))
        cutoff = now - 5.0
        while len(self._window) > 2 and self._window[1][0] <= cutoff:
            self._window.pop(0)
        if len(self._window) > 16:
            self._window = [self._window[0], *self._window[-15:]]
        base_t, base_bytes = self._window[0]
        dt = now - base_t
        if dt <= 0 or downloaded <= base_bytes:
            return None
        rate = (downloaded - base_bytes) / dt
        return round(rate, 2) if math.isfinite(rate) and rate >= 0 else None

    def update(
        self,
        stage: str,
        downloaded_bytes: int,
        total_bytes: int | None = None,
        *,
        force: bool = False,
        step: int | None = None,
        step_total: int | None = None,
        percent: float | None = None,
        detail: str | None = None,
    ) -> None:
        """Emit a progress line on stage transition, interval elapsed, or forced final update."""
        if not self.enabled:
            return
        now = time.monotonic()
        stage_changed = stage != self._last_stage
        step_changed = step is not None and step != self._last_step
        transition = stage_changed or step_changed
        if not force and not transition and self._last_emit_at is not None:
            if (now - self._last_emit_at) < self.min_interval_seconds:
                return
        downloaded = max(0, int(downloaded_bytes))
        total = int(total_bytes) if total_bytes is not None and total_bytes > 0 else None
        rate = self._compute_rate(stage, downloaded, now, stage_changed)
        payload: dict[str, object] = {
            "event": "transfer.progress",
            "stage": stage,
            "downloaded_bytes": downloaded,
            "total_bytes": total,
            "bytes_per_second": rate,
        }
        if step is not None:
            payload["step"] = max(0, int(step))
        if step_total is not None and int(step_total) > 0:
            payload["step_total"] = int(step_total)
        if percent is not None and math.isfinite(float(percent)):
            payload["percent"] = round(max(0.0, min(100.0, float(percent))), 2)
        if detail is not None:
            clean_detail = " ".join(str(detail).replace("\r", " ").replace("\n", " ").split())
            if clean_detail:
                payload["detail"] = clean_detail[:200]
        target = self._stream if self._stream is not None else sys.stderr
        target.write(json.dumps(payload, separators=(",", ":"), allow_nan=False) + "\n")
        target.flush()
        self._last_stage = stage
        if step is not None:
            self._last_step = max(0, int(step))
        self._last_emit_at = now


class ResumeDirectory:
    """Safely anchored existing partial output directory for --resume transfers."""

    def __init__(self, path: Path, identity: tuple[int, int]) -> None:
        self.path = path
        self.identity = identity

    def check(self) -> None:
        with NewDirectory.errors(), anchored(self.path, self.identity):
            pass

    def _open_existing_part_fd(self, tree, handle, name: str, flags: int) -> int:
        if os.name == "nt":
            return open_nofollow(self.path / name, flags)
        return os.open(name, flags | os.O_NOFOLLOW, 0o600, dir_fd=handle)

    def part_size(self, name: str) -> int:
        target = self.path / name
        if not os.path.lexists(target):
            return 0
        if linked(target):
            raise PreparationError("output_exists")
        info = target.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise PreparationError("output_exists")
        return info.st_size

    @contextmanager
    def open_part(self, name: str, *, append: bool):
        with NewDirectory.errors(), anchored(self.path, self.identity) as (tree, handle):
            target = self.path / name
            if os.path.lexists(target):
                if linked(target):
                    raise PreparationError("output_exists")
                fd = self._open_existing_part_fd(tree, handle, name, os.O_RDWR)
                try:
                    info = os.fstat(fd)
                    if not stat.S_ISREG(info.st_mode):
                        raise PreparationError("output_exists")
                except BaseException:
                    os.close(fd)
                    raise
                stream = os.fdopen(fd, "r+b")
                try:
                    if append:
                        stream.seek(0, os.SEEK_END)
                    else:
                        stream.seek(0)
                        stream.truncate(0)
                    yield stream
                    stream.flush()
                    os.fsync(stream.fileno())
                finally:
                    stream.close()
            else:
                fd = tree.file(handle, name)
                with os.fdopen(fd, "wb") as stream:
                    yield stream
                    stream.flush()
                    os.fsync(stream.fileno())

    @contextmanager
    def read_part(self, name: str):
        with NewDirectory.errors(), anchored(self.path, self.identity) as (tree, handle):
            fd = self._open_existing_part_fd(tree, handle, name, os.O_RDONLY)
            try:
                info = os.fstat(fd)
                if not stat.S_ISREG(info.st_mode):
                    raise PreparationError("output_exists")
            except BaseException:
                os.close(fd)
                raise
            with os.fdopen(fd, "rb") as stream:
                yield stream

    def json(self, relative: str, value) -> Path:
        target = self.path / relative
        if os.path.lexists(target):
            if linked(target) or not stat.S_ISREG(target.lstat().st_mode):
                raise PreparationError("output_exists")
            target.unlink()
        with NewDirectory.errors(), anchored(self.path, self.identity) as (tree, handle):
            fd = tree.file(handle, relative)
            with os.fdopen(fd, "wb") as stream:
                stream.write(json_bytes(value))
                stream.flush()
                os.fsync(stream.fileno())
        return target


def open_output_directory(
    out: Path,
    *,
    resume: bool,
    allowed_parts: set[str],
    final_names: set[str],
) -> NewDirectory | ResumeDirectory:
    """Create a fresh NewDirectory, or validate and anchor an existing partial directory."""
    if not resume or not os.path.lexists(out):
        return NewDirectory(out)
    if linked(out):
        raise PreparationError("output_exists")
    parent = out.parent.resolve(strict=True)
    if not parent.is_dir():
        raise NotADirectoryError(parent)
    resolved = parent / out.name
    with NewDirectory.errors(), anchored(resolved, create=False) as (tree, handle):
        identity = tree.identity(handle)
    with os.scandir(resolved) as entries:
        for entry in entries:
            if entry.name in final_names or entry.name not in allowed_parts:
                raise PreparationError("output_exists")
            entry_path = resolved / entry.name
            if linked(entry_path):
                raise PreparationError("output_exists")
            info = entry_path.lstat()
            if not stat.S_ISREG(info.st_mode):
                raise PreparationError("output_exists")
    return ResumeDirectory(resolved, identity)


def _unlink_part(directory: Path, part_name: str) -> None:
    try:
        (directory / part_name).unlink()
    except OSError:
        pass


def hash_part_file(output: NewDirectory | ResumeDirectory, part_name: str) -> tuple[int, str]:
    """Compute size and SHA-256 over a complete .part file from byte 0."""
    digest, size = hashlib.sha256(), 0
    if isinstance(output, ResumeDirectory):
        ctx = output.read_part(part_name)
    else:
        ctx = _read_new_directory_part(output, part_name)
    with ctx as stream:
        while chunk := stream.read(1024 * 1024):
            digest.update(chunk)
            size += len(chunk)
    return size, digest.hexdigest()


@contextmanager
def _read_new_directory_part(output: NewDirectory, part_name: str):
    with output.parent(Path()) as (_tree, handle):
        if os.name == "nt":
            fd = open_nofollow(output.path / part_name, os.O_RDONLY)
        else:
            fd = os.open(part_name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=handle)
        try:
            if not stat.S_ISREG(os.fstat(fd).st_mode):
                raise PreparationError("output_exists")
        except BaseException:
            os.close(fd)
            raise
        with os.fdopen(fd, "rb") as stream:
            yield stream


@contextmanager
def _open_part_stream(output: NewDirectory | ResumeDirectory, part_name: str, *, append: bool):
    if isinstance(output, ResumeDirectory):
        with output.open_part(part_name, append=append) as stream:
            yield stream
        return
    with output.stream(part_name) as stream:
        yield stream


def download_resumable_part(
    client,
    url: str,
    output: NewDirectory | ResumeDirectory,
    part_name: str,
    *,
    incomplete_reason: str,
    headers: dict[str, str] | None = None,
    resume: bool = False,
    expected_sha256: str | None = None,
    emitter: ProgressEmitter | None = None,
    base_offset: int = 0,
    combined_total: int | None = None,
    download_stage: str = "downloading",
    verify_stage: str = "verifying",
    step: int | None = None,
    step_total: int | None = None,
    detail: str | None = None,
) -> tuple[int, str]:
    """Stream one file into part_name with optional HTTP Range resume and full SHA-256."""
    offset = output.part_size(part_name) if (resume and isinstance(output, ResumeDirectory)) else 0
    if resume and expected_sha256 is not None and offset > 0:
        existing_size, existing_digest = hash_part_file(output, part_name)
        if existing_digest == expected_sha256:
            reported_total = combined_total if combined_total is not None else existing_size
            if emitter is not None:
                emitter.update(
                    verify_stage,
                    base_offset + existing_size,
                    reported_total,
                    step=step,
                    step_total=step_total,
                    detail=detail,
                )
            return existing_size, existing_digest

    req_headers = dict(headers or {"Accept-Encoding": "identity"})
    if resume and offset > 0:
        req_headers["Range"] = f"bytes={offset}-"

    skip_body = False
    resuming_append = False
    file_total: int | None = None
    with client.stream("GET", url, headers=req_headers) as opened:
        if resume and offset > 0 and opened.status_code == 206:
            encoding = (_header_value(opened.headers, "content-encoding") or "identity").lower()
            if encoding != "identity":
                raise PreparationError(
                    incomplete_reason,
                    details=failure_details("download_open", "unexpected_content_encoding"),
                )
            ok, range_total = parse_content_range(opened.headers, offset)
            if not ok:
                _unlink_part(output.path, part_name)
                raise PreparationError(
                    incomplete_reason,
                    details=failure_details("download_open", "transfer_error"),
                )
            part_len = parse_content_length(opened.headers)
            file_total = (
                range_total
                if range_total is not None
                else (offset + part_len if part_len is not None else None)
            )
            resuming_append = True
        elif resume and offset > 0 and opened.status_code == 416:
            total_416 = parse_unsatisfied_range_total(opened.headers)
            if total_416 is not None and total_416 == offset:
                skip_body = True
                file_total = offset
            else:
                _unlink_part(output.path, part_name)
                raise PreparationError(
                    incomplete_reason,
                    details=failure_details("download_open", "transfer_error"),
                )
        elif opened.status_code == 200:
            if (opened.headers.get("content-encoding", "identity")).lower() != "identity":
                raise PreparationError(
                    incomplete_reason,
                    details=failure_details("download_open", "unexpected_content_encoding"),
                )
            file_total = parse_content_length(opened.headers)
            offset = 0
            resuming_append = False
        else:
            raise PreparationError(
                incomplete_reason,
                details=failure_details("download_open", classify_http_status(opened.status_code)),
            )

        reported_total = combined_total if combined_total is not None else file_total
        if not skip_body:
            downloaded = offset
            if emitter is not None:
                emitter.update(
                    download_stage,
                    base_offset + downloaded,
                    reported_total,
                    step=step,
                    step_total=step_total,
                    detail=detail,
                )
            with _open_part_stream(output, part_name, append=resuming_append) as stream:
                try:
                    for chunk in opened.iter_raw():
                        stream.write(chunk)
                        downloaded += len(chunk)
                        if emitter is not None:
                            emitter.update(
                                download_stage,
                                base_offset + downloaded,
                                reported_total,
                                step=step,
                                step_total=step_total,
                                detail=detail,
                            )
                except Exception:
                    if emitter is not None and downloaded > offset:
                        emitter.update(
                            download_stage,
                            base_offset + downloaded,
                            reported_total,
                            step=step,
                            step_total=step_total,
                            detail=detail,
                            force=True,
                        )
                    raise
            if emitter is not None:
                emitter.update(
                    download_stage,
                    base_offset + downloaded,
                    reported_total,
                    step=step,
                    step_total=step_total,
                    detail=detail,
                    force=True,
                )

    reported_total = combined_total if combined_total is not None else file_total
    if emitter is not None:
        final_bytes = base_offset + (offset if skip_body else downloaded)
        emitter.update(
            verify_stage,
            final_bytes,
            reported_total or final_bytes,
            step=step,
            step_total=step_total,
            detail=detail,
        )

    size, actual = hash_part_file(output, part_name)
    if expected_sha256 is not None and actual != expected_sha256:
        _unlink_part(output.path, part_name)
        raise PreparationError(
            "engine_checksum_mismatch",
            details=failure_details("verify", "engine_checksum_mismatch"),
        )
    sync_directory(output.path)
    return size, actual
