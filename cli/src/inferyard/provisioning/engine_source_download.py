"""In-process engine acquisition: one URL, one directory, atomic rename."""

import hashlib
import math
import os
from pathlib import Path
import re
from datetime import UTC, datetime

import httpx

from inferyard.platforms.platform_io import sync_directory
from inferyard.provisioning.acquire_errors import (
    classify_http_status,
    classify_httpx_error,
    classify_os_error,
    failure_details,
)
from inferyard.provisioning.engine_source import DEFINITION, PARSER, parse_engine_source
from inferyard.provisioning.http_client import open_http_client, validate_proxy_origin
from inferyard.provisioning.preparation_io import PreparationError, new_path
from inferyard.provisioning.transfer_progress import (
    ProgressEmitter,
    download_resumable_part,
    open_output_directory,
    parse_content_length,
)

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


def download_to_stream(
    client,
    url,
    stream,
    *,
    expected_sha256=None,
    emitter=None,
    base_offset=0,
    combined_total=None,
):
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
        file_total = parse_content_length(opened.headers)
        reported_total = combined_total if combined_total is not None else file_total
        if emitter is not None:
            emitter.update("downloading", base_offset, reported_total)
        for chunk in opened.iter_raw():
            stream.write(chunk)
            digest.update(chunk)
            size += len(chunk)
            if emitter is not None:
                emitter.update("downloading", base_offset + size, reported_total)
        if emitter is not None:
            emitter.update("downloading", base_offset + size, reported_total, force=True)
    if emitter is not None:
        emitter.update("verifying", base_offset + size, reported_total or (base_offset + size))
    actual = digest.hexdigest()
    if expected is not None and actual != expected:
        raise PreparationError(
            "engine_checksum_mismatch",
            details=failure_details("verify", "engine_checksum_mismatch"),
        )
    return size, actual


def run_with_client(timeout_seconds, client, action, *, proxy_origin=None):
    validate_proxy_origin(proxy_origin)
    try:
        if client is not None:
            return action(client)
        with open_http_client(
            timeout_seconds,
            proxy_origin=proxy_origin,
            timeout_factory=_timeout,
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


def _download(client, source, out, *, emitter, resume):
    temporary = source.filename + ".part"
    output = open_output_directory(
        out,
        resume=resume,
        allowed_parts={temporary, "engine-source.json.part"},
        final_names={source.filename, "engine-source.json"},
    )
    try:
        size, digest = download_resumable_part(
            client,
            source.download_url,
            output,
            temporary,
            incomplete_reason=INCOMPLETE,
            resume=resume,
            emitter=emitter,
        )
        emitter.update("preparing", size, size)
        _publish(output, temporary, source.filename)
        output.json("engine-source.json.part", _record(source, size, digest))
        _publish(output, "engine-source.json.part", "engine-source.json")
    except BaseException:
        cleanup = ("engine-source.json.part",) if resume else (temporary, "engine-source.json.part")
        for part in cleanup:
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


def acquire(
    url,
    out,
    *,
    timeout_seconds=1800,
    client=None,
    progress_stream=False,
    resume=False,
    proxy_origin=None,
):
    source = parse_engine_source(url)
    validate_proxy_origin(proxy_origin)
    out = Path(out).absolute() if resume else new_path(out)
    emitter = ProgressEmitter(enabled=progress_stream)
    return run_with_client(
        timeout_seconds,
        client,
        lambda active: _download(active, source, out, emitter=emitter, resume=resume),
        proxy_origin=proxy_origin,
    )
