"""Frozen single-file source parsing and remote metadata validation."""

import hashlib
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from urllib.parse import quote, unquote, urlencode, urlsplit

from inferyard.config.preparation_io import PreparationError, read_json
from inferyard.contracts.validation import ContractError, strict_json_loads

DEFINITION = "remote_model_source.v1"
PARSER = "remote-model-source-parser.v1"
REVISION = re.compile(r"[0-9a-fA-F]{40}\Z")
SHA256 = re.compile(r"[0-9a-f]{64}\Z")
ENVIRONMENT_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]*\Z")
PLATFORMS = {"huggingface.co": "huggingface", "modelscope.cn": "modelscope"}
MISMATCH = "model_source_metadata_mismatch"


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
    def metadata_url(self):
        if self.platform == "huggingface":
            return (
                f"https://{self.host}/api/models/{self.repository}/revision/"
                f"{self.revision}?blobs=true"
            )
        parameters = {"Revision": self.revision, "Recursive": "False"}
        parent, separator, _ = self.path.rpartition("/")
        if separator:
            parameters["Root"] = parent
        query = urlencode(parameters)
        return f"https://{self.host}/api/v1/models/{self.repository}/repo/files?{query}"

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


def _file_metadata(source, data):
    if source.platform == "huggingface":
        if data.get("id") != source.repository or data.get("sha") != source.revision:
            raise ValueError
        files = data["siblings"]
        matches = [f for f in files if isinstance(f, dict) and f.get("rfilename") == source.path]
        if len(matches) != 1:
            raise ValueError
        entry = matches[0]
        size, digest = entry["size"], entry["lfs"]["sha256"]
        if type(entry["lfs"]["size"]) is not int or entry["lfs"]["size"] != size:
            raise ValueError
    else:
        if (
            type(data.get("Code")) is not int
            or data["Code"] != 200
            or data.get("Success") is not True
        ):
            raise ValueError
        files = data["Data"]["Files"]
        matches = [f for f in files if isinstance(f, dict) and f.get("Path") == source.path]
        if len(matches) != 1:
            raise ValueError
        entry = matches[0]
        if entry.get("Type") != "blob":
            raise ValueError
        size, digest = entry["Size"], entry["Sha256"]
    if not isinstance(files, list) or type(size) is not int or size <= 0:
        raise ValueError
    if not isinstance(digest, str) or not SHA256.fullmatch(digest):
        raise ValueError
    return size, digest


def parse_metadata(source, raw):
    try:
        data = strict_json_loads(raw.decode("utf-8"))
        size, digest = _file_metadata(source, data)
    except ContractError, ValueError, UnicodeError, KeyError, TypeError, AttributeError:
        raise PreparationError(MISMATCH) from None
    return {
        "definition": DEFINITION,
        "parser": PARSER,
        "platform": source.platform,
        "repository": source.repository,
        "revision": source.revision,
        "path": source.path,
        "bytes": size,
        "sha256": digest,
        "metadata_sha256": hashlib.sha256(raw).hexdigest(),
        "retrieved_at": datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z"),
    }


def source_declaration(record_path, model, *, repo=None, revision=None):
    """Check only an explicitly passed record against this command's file identity."""
    if record_path is None:
        return repo if repo is not None else "local", revision
    record = read_json(record_path, MISMATCH)
    try:
        if set(record) != {
            "definition",
            "parser",
            "platform",
            "repository",
            "revision",
            "path",
            "bytes",
            "sha256",
            "metadata_sha256",
            "retrieved_at",
        }:
            raise ValueError
        if record["definition"] != DEFINITION or record["parser"] != PARSER:
            raise ValueError
        host = {v: k for k, v in PLATFORMS.items()}[record["platform"]]
        source = parse_source(
            f"https://{host}/{record['repository']}/blob/"
            f"{record['revision']}/{quote(record['path'], safe='/')}"
        )
        if source.revision != record["revision"] or not SHA256.fullmatch(record["metadata_sha256"]):
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
