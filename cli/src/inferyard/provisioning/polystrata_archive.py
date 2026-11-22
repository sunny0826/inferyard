"""Safe PolyStrata runtime archive acquisition and extraction (tar.gz / zip)."""

from __future__ import annotations

import gzip
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import tarfile
import tempfile
import zipfile
import zlib

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

POLYSTRATA_PINNED_TAG = "v0.1.45-poly.10"
DEFAULT_POLYSTRATA_SOURCE_URL = (
    f"https://github.com/simplexai-labs/PolyStrata/archive/refs/tags/{POLYSTRATA_PINNED_TAG}.tar.gz"
)
SETUP_LOG_NAME = ".inferyard-setup.log"
INSTALLATION_FILE_NAME = "installation.json"
_RESERVED_ARCHIVE_NAMES = frozenset({SETUP_LOG_NAME.casefold(), INSTALLATION_FILE_NAME.casefold()})
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


def _strip_prefix_candidate(names: list[str]) -> str | None:
    if not names:
        return None
    roots = {name.split("/", 1)[0] for name in names}
    if len(roots) != 1:
        return None
    prefix = next(iter(roots))
    if any("/" not in name for name in names):
        return None
    if f"{prefix}/setup.py" in names:
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

    if "setup.py" not in files or "serve/server.py" not in files:
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
            if "setup.py" not in mapped or "serve/server.py" not in mapped:
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
    if resume and os.path.lexists(out):
        if linked(out) or not out.is_dir():
            raise PreparationError("output_exists")
        if (out / "setup.py").is_file() and (out / "serve" / "server.py").is_file():
            emitter.update(
                "downloading_runtime",
                0,
                None,
                step=2,
                step_total=4,
                percent=100.0,
                detail="reusing extracted runtime",
                force=True,
            )
            return True

    if local_archive is not None:
        size = local_archive.stat().st_size
        emitter.update(
            "downloading_runtime",
            size,
            size,
            step=2,
            step_total=4,
            percent=100.0,
            detail="extracting local archive",
            force=True,
        )
        if kind == "tar_gz":
            _extract_polystrata_tar(local_archive, out)
        else:
            _extract_polystrata_zip(local_archive, out)
        return False

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
                    final_names={"setup.py", INSTALLATION_FILE_NAME},
                )
                try:
                    download_resumable_part(
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
                    download_to_stream(
                        active_client,
                        remote_source.download_url,
                        stream,
                        expected_sha256=expected_sha256,
                        emitter=emitter,
                        download_stage="downloading_runtime",
                        verify_stage="downloading_runtime",
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
            if kind == "tar_gz":
                _extract_polystrata_tar(archive_path, out)
            else:
                _extract_polystrata_zip(archive_path, out)
        finally:
            shutil.rmtree(staging, ignore_errors=True)

    run_with_client(timeout_seconds, client, _download_and_extract, proxy_origin=proxy_origin)
    return False
