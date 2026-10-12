"""Engine archive URL parsing and the frozen acquisition record constants."""

import re
from dataclasses import dataclass
from urllib.parse import unquote, urlsplit

from inferyard.config.preparation_io import PreparationError

DEFINITION = "remote_engine_source.v1"
PARSER = "remote-engine-source-parser.v1"
HOST = re.compile(r"[a-z0-9]([a-z0-9.-]*[a-z0-9])?\Z")


@dataclass(frozen=True, slots=True)
class EngineSource:
    url: str
    filename: str

    @property
    def download_url(self):
        # A replaceable seam for loopback tests, mirroring ModelSource.
        return self.url


def parse_engine_source(url):
    try:
        if (
            not isinstance(url, str)
            or not url.isascii()
            or any(ord(c) < 33 or ord(c) == 127 for c in url)
            or "?" in url
            or "#" in url
            or "@" in url
        ):
            raise ValueError
        parsed = urlsplit(url)
        host = parsed.netloc.lower()
        if parsed.scheme != "https" or ":" in host or not HOST.fullmatch(host):
            raise ValueError
        parts = parsed.path.split("/")
        if len(parts) < 2 or parts[0] != "":
            raise ValueError
        for part in parts[1:]:
            if re.search(r"%(?![0-9a-fA-F]{2})", part):
                raise ValueError
        decoded = [unquote(part, errors="strict") for part in parts[1:]]
        for part in decoded:
            if (
                part in ("", ".", "..")
                or not part.isascii()
                or any(ord(c) < 32 or ord(c) == 127 or c in "/\\:" for c in part)
                or part != part.strip()
            ):
                raise ValueError
        return EngineSource(f"https://{host}{parsed.path}", decoded[-1])
    except ValueError, UnicodeError:
        raise PreparationError("invalid_engine_source") from None
