"""In-process engine acquisition: one URL, one new directory, one rename."""

import hashlib
import math
import os
import re
from datetime import UTC, datetime

import httpx

from inferyard.provisioning.acquire_errors import (
    classify_http_status,
    classify_httpx_error,
    classify_os_error,
    failure_details,
)
from inferyard.provisioning.preparation_io import NewDirectory, PreparationError, new_path
from inferyard.provisioning.engine_source import DEFINITION, PARSER, parse_engine_source
from inferyard.platforms.platform_io import sync_directory

INCOMPLETE = "engine_acquire_incomplete"


def validate_sha256(value):
    if value is None:
        return None
    if type(value) is not str or re.fullmatch(r"[0-9a-f]{64}", value) is None:
        raise PreparationError("invalid_input")
    return value


def _timeout(seconds):
    try:
        valid = type(seconds) in (int, float) and math.isfinite(seconds) and seconds > 0
    except OverflowError:
        valid = False
    if not valid:
        raise PreparationError("invalid_engine_source")
    return httpx.Timeout(seconds)


def _record(source, size, digest):
    return {
        "definition": DEFINITION,
        "parser": PARSER,
        "source_url": source.url,
        "bytes": size,
        "sha256": digest,
        "retrieved_at": datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z"),
    }


def _publish(output, temporary, final):
    os.rename(output.path / temporary, output.path / final)
    sync_directory(output.path)


def download_to_stream(client, url, stream, *, expected_sha256=None):
    expected = validate_sha256(expected_sha256)
    digest, size = hashlib.sha256(), 0
    with client.stream("GET", url, headers={"Accept-Encoding": "identity"}) as opened:
        if opened.status_code != 200:
            raise PreparationError(
                INCOMPLETE,
                details=failure_details("download_open", classify_http_status(opened.status_code)),
            )
        if opened.headers.get("content-encoding", "identity").lower() != "identity":
            raise PreparationError(
                INCOMPLETE,
                details=failure_details("download_open", "unexpected_content_encoding"),
            )
        for chunk in opened.iter_raw():
            stream.write(chunk)
            digest.update(chunk)
            size += len(chunk)
    actual = digest.hexdigest()
    if expected is not None and actual != expected:
        raise PreparationError(
            "engine_checksum_mismatch",
            details=failure_details("verify", "engine_checksum_mismatch"),
        )
    return size, actual


def run_with_client(timeout_seconds, client, action):
    try:
        if client is not None:
            return action(client)
        with httpx.Client(
            trust_env=False, follow_redirects=True, timeout=_timeout(timeout_seconds)
        ) as owned:
            return action(owned)
    except PreparationError:
        raise
    except httpx.HTTPError as exc:
        raise PreparationError(
            INCOMPLETE,
            details=failure_details("download_body", classify_httpx_error(exc)),
        ) from None
    except OSError as exc:
        raise PreparationError(
            "io_error",
            4,
            details=failure_details("disk_write", classify_os_error(exc)),
        ) from None


def _download(client, source, out):
    output = NewDirectory(out)
    temporary = source.filename + ".part"
    try:
        with output.stream(temporary) as stream:
            size, digest = download_to_stream(client, source.download_url, stream)
        _publish(output, temporary, source.filename)
        output.json("engine-source.json.part", _record(source, size, digest))
        _publish(output, "engine-source.json.part", "engine-source.json")
    except BaseException:
        for part in (temporary, "engine-source.json.part"):
            try:
                (output.path / part).unlink()
            except OSError:
                pass
        raise
    return {
        "out": str(output.path),
        "engine": str(output.path / source.filename),
        "source_record": str(output.path / "engine-source.json"),
        "bytes": size,
        "sha256": digest,
        "ready_to_run": False,
        "model_requests_sent": 0,
    }


def acquire(url, out, *, timeout_seconds=1800, client=None):
    source = parse_engine_source(url)
    out = new_path(out)
    return run_with_client(timeout_seconds, client, lambda active: _download(active, source, out))
