"""Safe PolyStrata runtime archive acquisition and extraction (tar.gz / zip)."""

from __future__ import annotations

import gzip
import hashlib
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import tarfile
import tempfile
import zipfile
import zlib

from inferyard.contracts.validation import strict_json_loads
from inferyard.platforms.platform_io import sync_directory
from inferyard.provisioning.acquire_errors import failure_details
from inferyard.provisioning.engine_prepare import _resolve_virtual_symlinks, _validate_tar_link
from inferyard.provisioning.engine_source import EngineSource, parse_engine_source
from inferyard.provisioning.engine_source_download import download_to_stream, run_with_client
from inferyard.provisioning.preparation_io import NewDirectory, PreparationError, linked
from inferyard.provisioning.runtime_archive import safe_member, zip_entries
from inferyard.provisioning.transfer_progress import (
    ProgressEmitter,
    download_resumable_part,
    open_output_directory,
)

POLYSTRATA_PINNED_TAG = "v0.2.20-poly.5"
POLYSTRATA_PINNED_COMMIT = "d62dfacbe185dfbb447e79c5fb1091ae189afc64"
DEFAULT_POLYSTRATA_SOURCE_URL = (
    f"https://github.com/simplexai-labs/PolyStrata/archive/{POLYSTRATA_PINNED_COMMIT}.tar.gz"
)
DEFAULT_POLYSTRATA_SOURCE_BYTES = 50443059
DEFAULT_POLYSTRATA_SOURCE_SHA256 = (
    "7618eb62d56daec9b3d4d41155d5e5c1f90f0b3278616e3cc18a38d87c753dbf"
)
SETUP_LOG_NAME = ".inferyard-setup.log"
INSTALLATION_FILE_NAME = "installation.json"
RUNTIME_RECEIPT_NAME = ".inferyard-runtime.json"
CONFIG_DIR_NAME = ".inferyard-config"
_RUNTIME_RECEIPT_DEFINITION = "polystrata-runtime.v1"
_RESERVED_ARCHIVE_NAMES = frozenset(
    {
        SETUP_LOG_NAME.casefold(),
        INSTALLATION_FILE_NAME.casefold(),
        RUNTIME_RECEIPT_NAME.casefold(),
        CONFIG_DIR_NAME.casefold(),
    }
)
_TAR_ERRORS = (tarfile.TarError, gzip.BadGzipFile, EOFError, zlib.error)


def _archive_kind(filename: str) -> str:
    lower = filename.lower()
    if lower.endswith((".tar.gz", ".tgz")):
        return "tar_gz"
    if lower.endswith(".zip"):
        return "zip"
    raise PreparationError("invalid_engine_source")


def resolve_polystrata_source(source: str | None) -> tuple[EngineSource | None, Path | None, str]:
    raw = DEFAULT_POLYSTRATA_SOURCE_URL if source is None else source
    if not isinstance(raw, str) or not raw.strip():
        raise PreparationError("invalid_engine_source")
    if "://" in raw:
        parsed = parse_engine_source(raw)
        return parsed, None, _archive_kind(parsed.filename)
    local_path = Path(raw).expanduser().absolute()
    try:
        info = local_path.stat()
    except OSError:
        raise PreparationError("invalid_engine_source") from None
    if not stat.S_ISREG(info.st_mode):
        raise PreparationError("invalid_engine_source")
    return None, local_path.resolve(strict=True), _archive_kind(local_path.name)


def _has_archive_layout(entries: set[str] | dict[str, zipfile.ZipInfo]) -> bool:
    return ("setup.py" in entries and "serve/server.py" in entries) or (
        "terminal/setup.py" in entries and "terminal/serve/server.py" in entries
    )


def _strip_prefix_candidate(names: list[str]) -> str | None:
    if not names:
        return None
    roots = {name.split("/", 1)[0] for name in names}
    if len(roots) != 1 or any("/" not in name for name in names):
        return None
    prefix = next(iter(roots))
    if f"{prefix}/setup.py" in names or f"{prefix}/terminal/setup.py" in names:
        return prefix + "/"
    return None


def _normalize_tar_name(raw_name: str, *, is_dir: bool) -> str | None:
    name = raw_name
    while name.startswith("./"):
        name = name[2:]
    if is_dir and name in ("", "."):
        return None
    return safe_member(name, directory=is_dir)


def _extract_polystrata_tar(archive_path: Path, out: Path) -> NewDirectory:
    try:
        with tarfile.open(archive_path, "r:gz") as package:
            members = package.getmembers()
    except _TAR_ERRORS:
        raise PreparationError("engine_unpack_failed") from None

    raw_all: list[str] = []
    raw_files: list[str] = []
    for member in members:
        safe_name = _normalize_tar_name(member.name, is_dir=member.isdir())
        if safe_name is not None:
            raw_all.append(safe_name)
            if not member.isdir():
                raw_files.append(safe_name)
    all_roots = {name.split("/", 1)[0] for name in raw_all}
    prefix = _strip_prefix_candidate(raw_files) if len(all_roots) == 1 else None

    def strip_name(safe_name: str | None) -> str | None:
        if safe_name is None:
            return None
        if prefix is None:
            return safe_name
        if safe_name == prefix[:-1]:
            return None
        if safe_name.startswith(prefix):
            return safe_name[len(prefix) :]
        return safe_name

    dirs, non_dirs, files, symlinks = {}, {}, set(), {}
    for member in members:
        safe_name = strip_name(_normalize_tar_name(member.name, is_dir=member.isdir()))
        if safe_name is None:
            continue
        folded = safe_name.casefold()
        if folded in _RESERVED_ARCHIVE_NAMES:
            raise PreparationError("unsafe_archive_entry")
        if member.isdir():
            if folded in non_dirs or (folded in dirs and dirs[folded] != safe_name):
                raise PreparationError("unsafe_archive_entry")
            dirs[folded] = safe_name
            continue
        if folded in dirs or folded in non_dirs:
            raise PreparationError("unsafe_archive_entry")
        if member.isreg():
            non_dirs[folded] = safe_name
            files.add(safe_name)
        elif member.issym():
            _validate_tar_link(safe_name, member.linkname)
            non_dirs[folded] = safe_name
            symlinks[safe_name] = member.linkname
        elif member.islnk():
            target = strip_name(_normalize_tar_name(member.linkname, is_dir=False))
            if target not in files:
                raise PreparationError("unsafe_archive_entry")
            non_dirs[folded] = safe_name
            files.add(safe_name)
        else:
            raise PreparationError("unsafe_archive_entry")

    if not _has_archive_layout(files):
        raise PreparationError("polystrata_runtime_invalid")
    _resolve_virtual_symlinks(symlinks)

    output = NewDirectory(out)
    try:
        with tarfile.open(archive_path, "r:gz") as package:
            for member in package:
                safe_name = strip_name(_normalize_tar_name(member.name, is_dir=member.isdir()))
                if safe_name is None:
                    continue
                if member.isdir():
                    output.directory(safe_name)
                elif member.isreg():
                    reader = package.extractfile(member)
                    if reader is None:
                        raise PreparationError("engine_unpack_failed")
                    with reader, output.stream(safe_name) as stream:
                        shutil.copyfileobj(reader, stream, 1024 * 1024)
                    if os.name != "nt" and (member.mode & 0o111):
                        os.chmod(output.path / safe_name, 0o755)
                elif member.islnk():
                    target = strip_name(_normalize_tar_name(member.linkname, is_dir=False))
                    assert target is not None
                    with (output.path / target).open("rb") as src, output.stream(safe_name) as dst:
                        shutil.copyfileobj(src, dst, 1024 * 1024)
                    if os.name != "nt" and os.access(output.path / target, os.X_OK):
                        os.chmod(output.path / safe_name, 0o755)
                elif member.issym():
                    rel = PurePosixPath(safe_name)
                    if rel.parent != PurePosixPath("."):
                        output.directory(str(rel.parent))
                    os.symlink(member.linkname, output.path / safe_name)
    except _TAR_ERRORS:
        raise PreparationError("engine_unpack_failed") from None
    output.check()
    root_resolved = output.path.resolve(strict=True)
    for rel_name in symlinks:
        try:
            resolved = (output.path / rel_name).resolve(strict=False)
        except OSError, RuntimeError:
            raise PreparationError("unsafe_archive_entry") from None
        if not resolved.is_relative_to(root_resolved):
            raise PreparationError("unsafe_archive_entry")
    return output


def _extract_polystrata_zip(archive_path: Path, out: Path) -> NewDirectory:
    try:
        with zipfile.ZipFile(archive_path) as package:
            entries = zip_entries(package)
            prefix = _strip_prefix_candidate(list(entries))
            mapped: dict[str, zipfile.ZipInfo] = {}
            for name, info in entries.items():
                rel = name[len(prefix) :] if prefix and name.startswith(prefix) else name
                if rel.casefold() in _RESERVED_ARCHIVE_NAMES:
                    raise PreparationError("unsafe_archive_entry")
                mapped[rel] = info
            if not _has_archive_layout(mapped):
                raise PreparationError("polystrata_runtime_invalid")
            output = NewDirectory(out)
            for rel, info in sorted(mapped.items()):
                with package.open(info) as src, output.stream(rel) as dst:
                    shutil.copyfileobj(src, dst, 1024 * 1024)
                if os.name != "nt" and ((info.external_attr >> 16) & 0o111):
                    os.chmod(output.path / rel, 0o755)
    except zipfile.BadZipFile:
        raise PreparationError("engine_unpack_failed") from None
    return output


def _is_regular_non_symlink(path: Path) -> bool:
    try:
        info = path.lstat()
    except OSError:
        return False
    return not linked(path) and stat.S_ISREG(info.st_mode)


def has_extracted_polystrata_files(out: Path) -> bool:
    if _is_regular_non_symlink(out / "setup.py") and _is_regular_non_symlink(
        out / "serve" / "server.py"
    ):
        return True
    term = out / "terminal"
    return (
        not linked(term)
        and term.is_dir()
        and _is_regular_non_symlink(term / "setup.py")
        and _is_regular_non_symlink(term / "serve" / "server.py")
    )


def _source_key(remote_source: EngineSource | None, local_archive: Path | None) -> str:
    if remote_source is not None:
        return remote_source.url
    assert local_archive is not None
    return f"local:{local_archive}"


def _read_runtime_receipt(out: Path) -> dict | None:
    receipt_path = out / RUNTIME_RECEIPT_NAME
    if not _is_regular_non_symlink(receipt_path):
        return None
    try:
        data = strict_json_loads(receipt_path.read_text(encoding="utf-8"))
    except (OSError, ValueError, UnicodeError):
        return None
    if (
        not isinstance(data, dict)
        or data.get("definition") != _RUNTIME_RECEIPT_DEFINITION
        or not isinstance(data.get("source"), str)
        or not isinstance(data.get("archive_sha256"), str)
    ):
        return None
    return data


def _copy_and_sha256(src: Path, dst: Path) -> tuple[int, str]:
    digest, size = hashlib.sha256(), 0
    with src.open("rb") as inp, dst.open("wb") as outp:
        while chunk := inp.read(1024 * 1024):
            outp.write(chunk)
            digest.update(chunk)
            size += len(chunk)
        outp.flush()
        os.fsync(outp.fileno())
    return size, digest.hexdigest()


def _extract_and_publish(
    archive_path: Path,
    out: Path,
    *,
    kind: str,
    source_id: str,
    archive_sha256: str,
    staging_dir: Path,
) -> None:
    """Extract into staging_dir/extracted, write receipt, then atomically publish to `out`."""
    if os.path.lexists(out):
        raise PreparationError("output_exists")
    extracted = staging_dir / "extracted"
    output = (
        _extract_polystrata_tar(archive_path, extracted)
        if kind == "tar_gz"
        else _extract_polystrata_zip(archive_path, extracted)
    )
    output.json(
        RUNTIME_RECEIPT_NAME,
        {
            "definition": _RUNTIME_RECEIPT_DEFINITION,
            "source": source_id,
            "archive_sha256": archive_sha256,
        },
    )
    sync_directory(output.path)
    try:
        os.rename(output.path, out)
    except OSError:
        raise PreparationError("output_exists") from None
    sync_directory(out.parent)


def acquire_runtime_tree(
    out: Path,
    *,
    remote_source: EngineSource | None,
    local_archive: Path | None,
    kind: str,
    expected_sha256: str | None,
    resume: bool,
    timeout_seconds: float,
    proxy_origin: str | None,
    client,
    emitter: ProgressEmitter,
) -> bool:
    """Ensure PolyStrata source files exist in `out`; return True if reused on resume."""
    source_id = _source_key(remote_source, local_archive)
    if local_archive is not None:
        staging = Path(tempfile.mkdtemp(prefix=".polystrata-prepare-", dir=out.parent))
        try:
            staged_archive = staging / local_archive.name
            local_size, local_digest = _copy_and_sha256(local_archive, staged_archive)
            if expected_sha256 is not None and local_digest != expected_sha256:
                raise PreparationError(
                    "engine_checksum_mismatch",
                    details=failure_details("verify", "engine_checksum_mismatch"),
                )
            if resume and os.path.lexists(out):
                if linked(out) or not out.is_dir():
                    raise PreparationError("output_exists")
                receipt = _read_runtime_receipt(out)
                if receipt is None or not has_extracted_polystrata_files(out):
                    raise PreparationError("output_exists")
                if receipt["archive_sha256"] != local_digest:
                    raise PreparationError(
                        "engine_checksum_mismatch",
                        details=failure_details("verify", "engine_checksum_mismatch"),
                    )
                if receipt["source"] != source_id:
                    raise PreparationError("output_exists")
                emitter.update(
                    "downloading_runtime", 0, None, step=2, step_total=4,
                    percent=100.0, detail="reusing extracted runtime", force=True,
                )
                return True
            emitter.update(
                "downloading_runtime", local_size, local_size, step=2, step_total=4,
                percent=100.0, detail="extracting local archive", force=True,
            )
            _extract_and_publish(
                staged_archive, out, kind=kind, source_id=source_id,
                archive_sha256=local_digest, staging_dir=staging,
            )
            return False
        finally:
            shutil.rmtree(staging, ignore_errors=True)

    if resume and os.path.lexists(out):
        if linked(out) or not out.is_dir():
            raise PreparationError("output_exists")
        receipt = _read_runtime_receipt(out)
        if receipt is not None:
            if not has_extracted_polystrata_files(out):
                raise PreparationError("output_exists")
            if expected_sha256 is not None and receipt["archive_sha256"] != expected_sha256:
                raise PreparationError(
                    "engine_checksum_mismatch",
                    details=failure_details("verify", "engine_checksum_mismatch"),
                )
            if receipt["source"] != source_id:
                raise PreparationError("output_exists")
            emitter.update(
                "downloading_runtime", 0, None, step=2, step_total=4,
                percent=100.0, detail="reusing extracted runtime", force=True,
            )
            return True

    assert remote_source is not None

    def _download_and_extract(active_client):
        staging = Path(tempfile.mkdtemp(prefix=".polystrata-prepare-", dir=out.parent))
        try:
            archive_path = staging / remote_source.filename
            if resume:
                part_name = remote_source.filename + ".part"
                output = open_output_directory(
                    out,
                    resume=True,
                    allowed_parts={part_name},
                    final_names={
                        "setup.py",
                        "polystrata.py",
                        INSTALLATION_FILE_NAME,
                        RUNTIME_RECEIPT_NAME,
                    },
                )
                try:
                    _, archive_digest = download_resumable_part(
                        active_client,
                        remote_source.download_url,
                        output,
                        part_name,
                        incomplete_reason="engine_acquire_incomplete",
                        resume=True,
                        expected_sha256=expected_sha256,
                        emitter=emitter,
                        download_stage="downloading_runtime",
                        verify_stage="downloading_runtime",
                        step=2,
                        step_total=4,
                        detail="downloading PolyStrata runtime",
                    )
                except PreparationError as exc:
                    if exc.reason == "engine_checksum_mismatch":
                        try:
                            os.rmdir(output.path)
                        except OSError:
                            pass
                    raise
                os.rename(output.path / part_name, archive_path)
                os.rmdir(output.path)
            else:
                with archive_path.open("wb") as stream:
                    _, archive_digest = download_to_stream(
                        active_client,
                        remote_source.download_url,
                        stream,
                        expected_sha256=expected_sha256,
                        emitter=emitter,
                        download_stage="downloading_runtime",
                        verify_stage="downloading_runtime",
                        step=2,
                        step_total=4,
                        detail="downloading PolyStrata runtime",
                    )
                    stream.flush()
                    os.fsync(stream.fileno())
            emitter.update(
                "downloading_runtime",
                archive_path.stat().st_size,
                archive_path.stat().st_size,
                step=2,
                step_total=4,
                percent=100.0,
                detail="extracting PolyStrata runtime",
                force=True,
            )
            _extract_and_publish(
                archive_path,
                out,
                kind=kind,
                source_id=source_id,
                archive_sha256=archive_digest,
                staging_dir=staging,
            )
        finally:
            shutil.rmtree(staging, ignore_errors=True)

    emitter.update(
        "downloading_runtime",
        0,
        None,
        step=2,
        step_total=4,
        detail="downloading PolyStrata runtime",
        force=True,
    )
    run_with_client(timeout_seconds, client, _download_and_extract, proxy_origin=proxy_origin)
    return False
