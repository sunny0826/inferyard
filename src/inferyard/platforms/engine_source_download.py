"""In-process engine acquisition: one URL, one new directory, one rename."""

import hashlib
import math
import os
from datetime import UTC, datetime

import httpx

from inferyard.config.preparation_io import NewDirectory, PreparationError, new_path
from inferyard.platforms.engine_source import DEFINITION, PARSER, parse_engine_source
from inferyard.platforms.platform_io import sync_directory

INCOMPLETE = "engine_acquire_incomplete"


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


def _download(client, source, out):
    output = NewDirectory(out)
    temporary = source.filename + ".part"
    digest, size = hashlib.sha256(), 0
    try:
        with client.stream(
            "GET", source.download_url, headers={"Accept-Encoding": "identity"}
        ) as opened:
            if opened.status_code != 200:
                raise PreparationError(INCOMPLETE)
            if opened.headers.get("content-encoding", "identity").lower() != "identity":
                raise PreparationError(INCOMPLETE)
            with output.stream(temporary) as stream:
                for chunk in opened.iter_raw():
                    stream.write(chunk)
                    digest.update(chunk)
                    size += len(chunk)
        _publish(output, temporary, source.filename)
        output.json("engine-source.json.part", _record(source, size, digest.hexdigest()))
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
        "sha256": digest.hexdigest(),
        "ready_to_run": False,
        "model_requests_sent": 0,
    }


def acquire(url, out, *, timeout_seconds=1800, client=None):
    source = parse_engine_source(url)
    out = new_path(out)
    try:
        if client is not None:
            return _download(client, source, out)
        with httpx.Client(
            trust_env=False, follow_redirects=True, timeout=_timeout(timeout_seconds)
        ) as owned:
            return _download(owned, source, out)
    except PreparationError:
        raise
    except httpx.HTTPError:
        raise PreparationError(INCOMPLETE) from None
    except OSError:
        raise PreparationError("io_error", 4) from None
