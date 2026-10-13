"""Frozen single-file source URL parsing and source-record validation."""

import math
import re
from dataclasses import dataclass
from datetime import datetime
from urllib.parse import quote, unquote, urlencode, urlsplit

from inferyard.config.preparation_io import PreparationError, read_json

DEFINITION = "remote_model_source.v1"
DEFINITION_V2 = "remote_model_source.v2"
DEFINITION_V3 = "remote_model_source.v3"
PARSER = "remote-model-source-parser.v1"
REVISION = re.compile(r"[0-9a-fA-F]{40}\Z")
SHA256 = re.compile(r"[0-9a-f]{64}\Z")
ENVIRONMENT_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]*\Z")
PLATFORMS = {"huggingface.co": "huggingface", "modelscope.cn": "modelscope"}
MISMATCH = "model_source_metadata_mismatch"
BUDGET_KEYS = frozenset({"total_seconds", "read_seconds", "cleanup_seconds", "max_bytes"})


@dataclass(frozen=True, slots=True)
class ModelSource:
    platform: str
    repository: str
    revision: str
    path: str

    @property
    def host(self):
        return "huggingface.co" if self.platform == "huggingface" else "modelscope.cn"

    @property
    def download_url(self):
        if self.platform == "huggingface":
            return (
                f"https://{self.host}/{self.repository}/resolve/{self.revision}/"
                f"{quote(self.path, safe='/')}"
            )
        query = urlencode({"Revision": self.revision, "FilePath": self.path})
        return f"https://{self.host}/api/v1/models/{self.repository}/repo?{query}"


def parse_source(url):
    try:
        if (
            not isinstance(url, str)
            or not url.isascii()
            or any(ord(c) < 33 or ord(c) == 127 for c in url)
            or "?" in url
            or "#" in url
        ):
            raise ValueError
        parsed = urlsplit(url)
        host = parsed.netloc.lower()
        if parsed.scheme != "https" or host not in PLATFORMS:
            raise ValueError
        parts = parsed.path.split("/")
        if len(parts) < 6 or parts[0] != "" or parts[3] != "blob":
            raise ValueError
        if not REVISION.fullmatch(parts[4]):
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
        owner, repo = decoded[:2]
        if any(not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", p) for p in (owner, repo)):
            raise ValueError
        path = "/".join(decoded[4:])
        if not path.endswith(".gguf"):
            raise ValueError
        return ModelSource(PLATFORMS[host], f"{owner}/{repo}", parts[4].lower(), path)
    except ValueError, UnicodeError:
        raise PreparationError("invalid_model_source") from None


def validate_token_env(name):
    if name is not None and (not isinstance(name, str) or not ENVIRONMENT_NAME.fullmatch(name)):
        raise PreparationError("invalid_model_source")


def _legacy_budget(value):
    """Validate the v2 record's frozen acquisition budget without constructing it."""
    if not isinstance(value, dict) or set(value) != BUDGET_KEYS:
        raise ValueError
    for key in ("total_seconds", "read_seconds", "cleanup_seconds"):
        seconds = value[key]
        try:
            valid = type(seconds) in (int, float) and math.isfinite(seconds) and seconds > 0
        except OverflowError:
            valid = False
        if not valid:
            raise ValueError
    if type(value["max_bytes"]) is not int or not 0 < value["max_bytes"] <= 20 * 1024**3:
        raise ValueError
    return value["max_bytes"]


def source_declaration(record_path, model, *, repo=None, revision=None):
    """Check only an explicitly passed record against this command's file identity."""
    if record_path is None:
        return repo if repo is not None else "local", revision
    record = read_json(record_path, MISMATCH)
    try:
        expected = {
            "definition",
            "parser",
            "platform",
            "repository",
            "revision",
            "path",
            "bytes",
            "sha256",
            "retrieved_at",
        }
        definition = record["definition"]
        if definition == DEFINITION_V3:
            pass
        elif definition in (DEFINITION, DEFINITION_V2):
            expected.add("metadata_sha256")
            if definition == DEFINITION_V2:
                expected.add("acquisition_budget")
                if type(record["bytes"]) is not int or record["bytes"] > _legacy_budget(
                    record["acquisition_budget"]
                ):
                    raise ValueError
        else:
            raise ValueError
        if set(record) != expected or record["parser"] != PARSER:
            raise ValueError
        host = {v: k for k, v in PLATFORMS.items()}[record["platform"]]
        source = parse_source(
            f"https://{host}/{record['repository']}/blob/"
            f"{record['revision']}/{quote(record['path'], safe='/')}"
        )
        if source.revision != record["revision"]:
            raise ValueError
        if definition != DEFINITION_V3 and not SHA256.fullmatch(record["metadata_sha256"]):
            raise ValueError
        if type(record["bytes"]) is not int or record["bytes"] != model.size:
            raise ValueError
        if record["sha256"] != model.sha256 or not model.unchanged():
            raise ValueError
        datetime.strptime(record["retrieved_at"], "%Y-%m-%dT%H:%M:%SZ")
        if (repo is not None and repo != source.repository) or (
            revision is not None and revision != source.revision
        ):
            raise ValueError
    except ValueError, KeyError, TypeError, AttributeError, PreparationError:
        raise PreparationError(MISMATCH) from None
    return source.repository, source.revision
