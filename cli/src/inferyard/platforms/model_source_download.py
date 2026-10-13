"""In-process streaming acquisition: one pinned URL, one new directory, one rename."""

import hashlib
import math
import os
from datetime import UTC, datetime

import httpx

from inferyard.config.preparation_io import NewDirectory, PreparationError, new_path
from inferyard.platforms.model_source import DEFINITION_V3, PARSER, parse_source, validate_token_env
from inferyard.platforms.platform_io import sync_directory

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
        raise PreparationError(INCOMPLETE)
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


def _download(client, source, out, token):
    output = NewDirectory(out)
    name = source.path.rsplit("/", 1)[-1]
    temporary = name + ".part"
    digest, size = hashlib.sha256(), 0
    headers = {"Accept-Encoding": "identity"}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    try:
        with client.stream("GET", source.download_url, headers=headers) as opened:
            if opened.status_code != 200:
                raise PreparationError(INCOMPLETE)
            if opened.headers.get("content-encoding", "identity").lower() != "identity":
                raise PreparationError(INCOMPLETE)
            with output.stream(temporary) as stream:
                for chunk in opened.iter_raw():
                    stream.write(chunk)
                    digest.update(chunk)
                    size += len(chunk)
        _publish(output, temporary, name)
        output.json("source.json.part", _record(source, size, digest.hexdigest()))
        _publish(output, "source.json.part", "source.json")
    except BaseException:
        for part in (temporary, "source.json.part"):
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
        "sha256": digest.hexdigest(),
        "ready_to_run": False,
        "model_requests_sent": 0,
    }


def acquire(url, out, token_env=None, *, timeout_seconds=1800, client=None):
    source = parse_source(url)
    token = _token(token_env)
    out = new_path(out)
    try:
        if client is not None:
            return _download(client, source, out, token)
        with httpx.Client(
            trust_env=False, follow_redirects=True, timeout=_timeout(timeout_seconds)
        ) as owned:
            return _download(owned, source, out, token)
    except PreparationError:
        raise
    except httpx.HTTPError:
        raise PreparationError(INCOMPLETE) from None
    except OSError:
        raise PreparationError("io_error", 4) from None
