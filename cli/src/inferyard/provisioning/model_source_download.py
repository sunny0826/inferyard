"""In-process streaming acquisition: one pinned URL, one directory, atomic rename."""

import math
import os
from datetime import UTC, datetime
from pathlib import Path

import httpx

from inferyard.platforms.platform_io import sync_directory
from inferyard.provisioning.acquire_errors import (
    classify_httpx_error,
    classify_os_error,
    failure_details,
)
from inferyard.provisioning.http_client import open_http_client, origin_of, validate_proxy_origin
from inferyard.provisioning.model_source import (
    DEFINITION_V3,
    PARSER,
    parse_source,
    validate_token_env,
)
from inferyard.provisioning.preparation_io import PreparationError, new_path
from inferyard.provisioning.transfer_progress import (
    ProgressEmitter,
    download_resumable_part,
    open_output_directory,
)

INCOMPLETE = "model_acquire_incomplete"


def _timeout(seconds):
    try:
        valid = type(seconds) in (int, float) and math.isfinite(seconds) and seconds > 0
    except OverflowError:
        valid = False
    if not valid:
        raise PreparationError("invalid_model_source")
    return httpx.Timeout(seconds)


def _token(name):
    validate_token_env(name)
    if name is None:
        return None
    value = os.environ.get(name)
    if not value or not value.isascii() or any(ord(c) < 33 or ord(c) == 127 for c in value):
        raise PreparationError(
            INCOMPLETE,
            details=failure_details("token", "token_env_missing"),
        )
    return value


def _record(source, size, digest):
    return {
        "definition": DEFINITION_V3,
        "parser": PARSER,
        "platform": source.platform,
        "repository": source.repository,
        "revision": source.revision,
        "path": source.path,
        "bytes": size,
        "sha256": digest,
        "retrieved_at": datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z"),
    }


def _publish(output, temporary, final):
    os.rename(output.path / temporary, output.path / final)
    sync_directory(output.path)


def _download(client, source, out, token, *, emitter, resume):
    name = source.path.rsplit("/", 1)[-1]
    temporary = name + ".part"
    output = open_output_directory(
        out,
        resume=resume,
        allowed_parts={temporary, "source.json.part"},
        final_names={name, "source.json"},
    )
    headers = {"Accept-Encoding": "identity"}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    try:
        size, digest = download_resumable_part(
            client,
            source.download_url,
            output,
            temporary,
            incomplete_reason=INCOMPLETE,
            headers=headers,
            resume=resume,
            emitter=emitter,
        )
        emitter.update("preparing", size, size)
        _publish(output, temporary, name)
        output.json("source.json.part", _record(source, size, digest))
        _publish(output, "source.json.part", "source.json")
    except BaseException:
        cleanup = ("source.json.part",) if resume else (temporary, "source.json.part")
        for part in cleanup:
            try:
                (output.path / part).unlink()
            except OSError:
                pass
        raise
    return {
        "out": str(output.path),
        "model": str(output.path / name),
        "source_record": str(output.path / "source.json"),
        "bytes": size,
        "sha256": digest,
        "ready_to_run": False,
        "model_requests_sent": 0,
    }


def acquire(
    url,
    out,
    token_env=None,
    *,
    timeout_seconds=1800,
    client=None,
    progress_stream=False,
    resume=False,
    proxy_origin=None,
):
    source = parse_source(url)
    validate_proxy_origin(proxy_origin)
    token = _token(token_env)
    out = Path(out).absolute() if resume else new_path(out)
    emitter = ProgressEmitter(enabled=progress_stream)
    try:
        if client is not None:
            return _download(client, source, out, token, emitter=emitter, resume=resume)
        auth_origin = origin_of(source.download_url) if token is not None else None
        with open_http_client(
            timeout_seconds,
            proxy_origin=proxy_origin,
            auth_origin=auth_origin,
            timeout_factory=_timeout,
        ) as owned:
            return _download(owned, source, out, token, emitter=emitter, resume=resume)
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
