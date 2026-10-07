"""Append-only evidence, atomic snapshots, secret redaction, and strict offline reads."""

from __future__ import annotations

import hashlib
import json
import os
import time
import uuid
from datetime import UTC, datetime
from pathlib import Path, PureWindowsPath

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document
from inferyard.platforms.platform_io import (
    filesystem_path,
    legacy_request_alias,
    open_nofollow,
    publish,
    resolved_within,
    sync_directory,
)


class EvidenceError(RuntimeError):
    pass


class Redactor:
    def __init__(self, secrets=()):
        self.secrets = tuple(sorted({value for value in secrets if value}, key=len, reverse=True))
        self.changed = False

    def text(self, value: str) -> str:
        for secret in self.secrets:
            if secret in value:
                self.changed = True
                value = value.replace(secret, "[REDACTED]")
        return value

    def clean(self, value):
        if isinstance(value, str):
            return self.text(value)
        if isinstance(value, list):
            return [self.clean(item) for item in value]
        if isinstance(value, tuple):
            return [self.clean(item) for item in value]
        if isinstance(value, dict):
            return {self.text(key): self.clean(item) for key, item in value.items()}
        return value

    def stream(self):
        return StreamRedactor(self)


class StreamRedactor:
    """Hold a possible secret prefix until a later chunk disambiguates it."""

    def __init__(self, redactor: Redactor):
        self.redactor = redactor
        self.pending = ""
        self.changed = False
        self.max_secret_length = max(map(len, redactor.secrets), default=0)

    def feed(self, chunk: str, *, final=False) -> str:
        if not self.redactor.secrets:
            return chunk
        text = self.pending + chunk
        self.pending = ""
        result = []
        index = 0
        while index < len(text):
            remaining_length = len(text) - index
            match = next((s for s in self.redactor.secrets if text.startswith(s, index)), None)
            if match:
                result.append("[REDACTED]")
                self.changed = self.redactor.changed = True
                index += len(match)
            elif (
                not final
                and remaining_length < self.max_secret_length
                and any(
                    remaining_length < len(secret)
                    and all(
                        text[index + offset] == secret[offset] for offset in range(remaining_length)
                    )
                    for secret in self.redactor.secrets
                )
            ):
                # Only copy a bounded suffix, never the whole remaining chunk.
                self.pending = text[index:]
                break
            else:
                result.append(text[index])
                index += 1
        return "".join(result)


def json_bytes(value) -> bytes:
    return (
        json.dumps(
            value, ensure_ascii=False, allow_nan=False, sort_keys=True, separators=(",", ":")
        )
        + "\n"
    ).encode("utf-8")


def sha256_file(path: Path) -> str:
    with filesystem_path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def fsync_directory(path: Path) -> None:
    sync_directory(path)


def atomic_bytes(path: Path, content: bytes, *, overwrite=False) -> None:
    """Publish fully synced bytes. link() provides no-overwrite atomic publication."""
    path = filesystem_path(path)
    temporary = path.parent / f".{uuid.uuid4().hex}.tmp"
    try:
        fd = open_nofollow(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
        with os.fdopen(fd, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        publish(temporary, path, overwrite)
        fsync_directory(path.parent)
    except OSError as exc:
        raise EvidenceError("atomic_write_failed") from exc
    finally:
        temporary.unlink(missing_ok=True)


def local_file(root: Path, relative: str) -> Path:
    # Renamed historical snapshots remain bound by the original manifest keys.
    # Resolve only known filename grammars; metadata cannot supply arbitrary paths.
    name = Path(relative)
    colon_alias = legacy_request_alias(relative)
    if (
        name.is_absolute()
        or (PureWindowsPath(relative).drive and colon_alias is None)
        or ".." in name.parts
        or not name.parts
    ):
        raise EvidenceError("unsafe_evidence_path")
    if relative.endswith(".request.json"):
        original_usable = os.name != "nt" or ":" not in relative
        if not original_usable or not filesystem_path(root / relative).is_file():
            from inferyard.evidence.request_snapshots import migrated_snapshot_path

            migrated = migrated_snapshot_path(root, relative)
            if migrated is not None and filesystem_path(migrated).is_file():
                if not resolved_within(migrated, root):
                    raise EvidenceError("unsafe_evidence_symlink")
                return filesystem_path(migrated)
    alias = legacy_request_alias(relative)
    if alias is not None and (os.name == "nt" or not filesystem_path(root / relative).exists()):
        # Never open an NTFS alternate data stream. Only the exact historical
        # request-name grammar has an alias, created by the archival copy tool.
        candidate = local_file(root, alias)
        if not candidate.is_file():
            raise EvidenceError("portable_legacy_request_missing")
        return candidate
    name = Path(relative)
    if (
        name.is_absolute()
        or PureWindowsPath(relative).drive
        or (os.name == "nt" and ":" in relative)
        or ".." in name.parts
        or not name.parts
    ):
        raise EvidenceError("unsafe_evidence_path")
    candidate = root / name
    if not resolved_within(candidate, root):
        raise EvidenceError("unsafe_evidence_symlink")
    return filesystem_path(candidate)


def read_json(path: Path):
    try:
        return strict_json_loads(filesystem_path(path).read_text(encoding="utf-8"))
    except (OSError, UnicodeError, ContractError) as exc:
        raise EvidenceError("invalid_json_evidence") from exc


def read_jsonl(path: Path) -> tuple[list[dict], list[str]]:
    records, limitations = [], []
    try:
        with filesystem_path(path).open("rb") as stream:
            offset = 0
            for line in stream:
                if not line.endswith(b"\n"):
                    limitations.append(f"truncated_tail_at_byte:{offset}")
                    break
                record = strict_json_loads(line.decode("utf-8"))
                if not isinstance(record, dict):
                    raise EvidenceError("invalid_jsonl_record")
                records.append(record)
                offset += len(line)
    except (OSError, UnicodeError, ContractError) as exc:
        raise EvidenceError("corrupt_jsonl_evidence") from exc
    return records, limitations


class EvidenceStore:
    def __init__(self, root: Path, redactor: Redactor | None = None, *, run_id: str | None = None):
        self.redactor = redactor or Redactor()
        self.run_id = run_id or datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ-") + uuid.uuid4().hex
        if Path(self.run_id).name != self.run_id or self.run_id in ("", ".", ".."):
            raise EvidenceError("invalid_run_id")
        filesystem_path(root).mkdir(parents=True, exist_ok=True)
        self.path = root / self.run_id
        filesystem_path(self.path).mkdir(mode=0o700)  # never append to an older run.
        fsync_directory(root)
        self.clock_id = "clock-" + uuid.uuid4().hex
        self._seq = {"events.jsonl": 0, "memory.jsonl": 0}
        self._logs = {}
        self._pending_sync = set()
        self._last_flush = time.monotonic()
        self.sealed = False
        for filename in (
            "events.jsonl",
            "memory.jsonl",
            "environment.jsonl",
            "schedule.jsonl",
        ):
            fd = open_nofollow(self.path / filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
            self._logs[filename] = os.fdopen(fd, "wb")
            self._pending_sync.add(filename)
        fsync_directory(self.path)

    def snapshot(self, name: str, value) -> None:
        if self.sealed:
            raise EvidenceError("sealed_run")
        atomic_bytes(local_file(self.path, name), json_bytes(self.redactor.clean(value)))

    def text_snapshot(self, name: str, value: str) -> None:
        if self.sealed:
            raise EvidenceError("sealed_run")
        atomic_bytes(local_file(self.path, name), self.redactor.text(value).encode("utf-8"))

    def _append(self, name: str, value, *, sync=False):
        if self.sealed:
            raise EvidenceError("sealed_run")
        try:
            self._logs[name].write(json_bytes(self.redactor.clean(value)))
            self._pending_sync.add(name)
            if sync:
                self.flush(sync=True)
            else:
                self.flush_due()
        except (OSError, ValueError) as exc:
            raise EvidenceError("evidence_append_failed") from exc

    def observation(self, name: str, value: dict):
        if name not in ("environment.jsonl", "schedule.jsonl"):
            raise EvidenceError("invalid_observation_log")
        self._append(name, value)

    def flush_due(self):
        if time.monotonic() - self._last_flush >= 0.5:
            self.flush(sync=True)

    def flush(self, *, sync=False):
        try:
            for name, stream in self._logs.items():
                if name in self._pending_sync and not stream.closed:
                    stream.flush()
                    if sync:
                        os.fsync(stream.fileno())
                        self._pending_sync.remove(name)
            self._last_flush = time.monotonic()
        except OSError as exc:
            raise EvidenceError("evidence_flush_failed") from exc

    def seal(self, extra: dict | None = None):
        self.flush(sync=True)
        files = {}
        for path in sorted(filesystem_path(self.path).iterdir()):
            if path.is_file() and path.name != "manifest.json" and not path.name.startswith("."):
                files[path.name] = {
                    "sha256": sha256_file(path),
                    "bytes": path.stat().st_size,
                    "derived": path.name in ("summary.json", "report.html", "requests.jsonl"),
                }
        manifest = {
            "schema_version": SCHEMA_VERSION,
            "run_id": self.run_id,
            "sealed": True,
            "files": files,
            **(extra or {}),
        }
        validate_document("manifest", manifest)
        self.snapshot("manifest.json", manifest)
        self.sealed = True

    def close(self):
        try:
            self.flush(sync=True)
        finally:
            for stream in self._logs.values():
                stream.close()


def verify_manifest(root: Path, *, _manifest=None, _observed=None) -> list[str]:
    limitations = []
    manifest_path = local_file(root, "manifest.json")
    if not manifest_path.exists():
        return ["manifest_missing_unsealed_run"]
    manifest = _manifest if _manifest is not None else read_json(manifest_path)
    if (
        not isinstance(manifest, dict)
        or manifest.get("schema_version") != SCHEMA_VERSION
        or manifest.get("sealed") is not True
        or not isinstance(manifest.get("files"), dict)
    ):
        raise EvidenceError("invalid_manifest")
    try:
        validate_document("manifest", manifest)
    except ContractError as exc:
        raise EvidenceError("invalid_manifest") from exc
    for name, info in manifest["files"].items():
        if name == "manifest.json" or not isinstance(info, dict):
            raise EvidenceError("invalid_manifest_entry")
        path = local_file(root, name)
        if _observed is not None and name in _observed:
            size, digest = _observed[name]
            valid = size == info.get("bytes") and digest == info.get("sha256")
        else:
            valid = path.is_file() and path.stat().st_size == info.get("bytes")
            valid = valid and sha256_file(path) == info.get("sha256")
        if not valid:
            # Classification is code-owned; do not trust a tampered 'derived' flag.
            if name in ("summary.json", "report.html", "requests.jsonl"):
                limitations.append(f"derived_evidence_damaged:{name}")
            else:
                raise EvidenceError("original_evidence_hash_mismatch")
    return limitations
