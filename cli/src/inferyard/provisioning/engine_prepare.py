"""Download, verify, and unpack one engine archive or standalone binary into a new tree."""

import gzip
import os
import shutil
import tarfile
import tempfile
import zipfile
import zlib
from datetime import UTC, datetime
from pathlib import Path, PurePosixPath, PureWindowsPath
from types import SimpleNamespace

from inferyard.platforms.platform_io import sync_directory
from inferyard.provisioning import runtime_profiles, windows_runtime_prepare
from inferyard.provisioning.engine_source import DEFINITION, PARSER, parse_engine_source
from inferyard.provisioning.engine_source_download import (
    _timeout,
    download_to_stream,
    run_with_client,
    validate_sha256,
)
from inferyard.provisioning.preparation_io import (
    NewDirectory,
    PreparationError,
    new_path,
    require_platform,
)
from inferyard.provisioning.runtime_archive import safe_member, zip_entries

_ENGINE_BINARIES = ("llama-server", "llama-server.exe")
_TAR_ERRORS = (tarfile.TarError, gzip.BadGzipFile, EOFError, zlib.error)


def _archive_kind(filename):
    lower = filename.lower()
    if lower.endswith((".tar.gz", ".tgz")):
        return "tar_gz"
    if lower.endswith(".zip"):
        return "zip"
    if filename in _ENGINE_BINARIES:
        return "binary"
    raise PreparationError("engine_binary_missing")


def _select_engine_binary(relative_paths):
    preferred = "llama-server.exe" if os.name == "nt" else "llama-server"
    candidates = [
        rel for rel in relative_paths if PurePosixPath(rel).name.lower() in _ENGINE_BINARIES
    ]
    if not candidates:
        raise PreparationError("engine_binary_missing")
    preferred_matches = sorted(
        rel for rel in candidates if PurePosixPath(rel).name.lower() == preferred
    )
    return preferred_matches[0] if preferred_matches else sorted(candidates)[0]


def _validate_runtime_profile(
    runtime_profile, has_runtime_url, expected_sha256, runtime_expected_sha256
):
    require_platform(("Windows", "x64"))
    profile, _ = runtime_profiles.load_profile(runtime_profile)
    if profile["mode"] == "cuda" and not has_runtime_url:
        raise PreparationError("runtime_archive_required")
    if profile["mode"] == "cpu" and has_runtime_url:
        raise PreparationError("runtime_archive_forbidden")
    by_role = {archive["role"]: archive for archive in profile["archives"]}
    if expected_sha256 is not None and expected_sha256 != by_role["engine"]["sha256"]:
        raise PreparationError("engine_checksum_mismatch")
    if (
        runtime_expected_sha256 is not None
        and "runtime" in by_role
        and runtime_expected_sha256 != by_role["runtime"]["sha256"]
    ):
        raise PreparationError("engine_checksum_mismatch")
    return profile


def _download_staged(client, url, path, expected_sha256):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("wb") as stream:
        size, digest = download_to_stream(client, url, stream, expected_sha256=expected_sha256)
        stream.flush()
        os.fsync(stream.fileno())
    return size, digest


def _normalize_tar_name(raw_name, *, is_dir):
    name = raw_name
    while name.startswith("./"):
        name = name[2:]
    if is_dir and name in ("", "."):
        return None
    return safe_member(name, directory=is_dir)


def _validate_tar_link(member_path, linkname):
    if not isinstance(linkname, str) or not linkname or "\x00" in linkname or "\\" in linkname:
        raise PreparationError("unsafe_archive_entry")
    if PurePosixPath(linkname).is_absolute() or PureWindowsPath(linkname).drive:
        raise PreparationError("unsafe_archive_entry")
    stack = list(PurePosixPath(member_path).parent.parts)
    for part in linkname.split("/"):
        if part in ("", "."):
            continue
        if part == "..":
            if not stack:
                raise PreparationError("unsafe_archive_entry")
            stack.pop()
        else:
            stack.append(part)


def _resolve_virtual_symlinks(symlinks):
    folded_map = {path.casefold(): target for path, target in symlinks.items()}
    for start in symlinks:
        current = []
        pending = list(start.split("/"))
        hops = 0
        while pending:
            part = pending.pop(0)
            if part in ("", "."):
                continue
            if part == "..":
                if not current:
                    raise PreparationError("unsafe_archive_entry")
                current.pop()
                continue
            current.append(part)
            key = "/".join(current).casefold()
            if key in folded_map:
                hops += 1
                if hops > 40:
                    raise PreparationError("unsafe_archive_entry")
                current.pop()
                pending = folded_map[key].split("/") + pending


def _scan_tar_members(archive_path):
    dirs, non_dirs, files, symlinks = {}, {}, set(), {}
    try:
        with tarfile.open(archive_path, "r:gz") as package:
            members = package.getmembers()
    except _TAR_ERRORS:
        raise PreparationError("engine_unpack_failed") from None
    for member in members:
        safe_name = _normalize_tar_name(member.name, is_dir=member.isdir())
        if safe_name is None:
            continue
        folded = safe_name.casefold()
        if folded == "engine-prepare.json":
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
            target = _normalize_tar_name(member.linkname, is_dir=False)
            if target not in files:
                raise PreparationError("unsafe_archive_entry")
            non_dirs[folded] = safe_name
            files.add(safe_name)
        else:
            raise PreparationError("unsafe_archive_entry")
    parents = {}
    for name in [*dirs.values(), *non_dirs.values()]:
        parts = name.split("/")
        for i in range(1, len(parts)):
            prefix = "/".join(parts[:i])
            if prefix.casefold() in non_dirs:
                raise PreparationError("unsafe_archive_entry")
            if parents.setdefault(prefix.casefold(), prefix) != prefix:
                raise PreparationError("unsafe_archive_entry")
    _resolve_virtual_symlinks(symlinks)
    return _select_engine_binary(files), set(symlinks)


def _extract_tar(archive_path, out):
    engine_rel, symlinks = _scan_tar_members(archive_path)
    output = NewDirectory(out)
    try:
        with tarfile.open(archive_path, "r:gz") as package:
            for member in package:
                safe_name = _normalize_tar_name(member.name, is_dir=member.isdir())
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
                    target = _normalize_tar_name(member.linkname, is_dir=False)
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
    for rel in symlinks:
        try:
            resolved = (output.path / rel).resolve(strict=False)
        except OSError, RuntimeError:
            raise PreparationError("unsafe_archive_entry") from None
        if not resolved.is_relative_to(root_resolved):
            raise PreparationError("unsafe_archive_entry")
    return output, output.path / engine_rel


def _extract_zip(archive_path, out):
    try:
        with zipfile.ZipFile(archive_path) as package:
            entries = zip_entries(package)
            if any(name.casefold() == "engine-prepare.json" for name in entries):
                raise PreparationError("unsafe_archive_entry")
            engine_rel = _select_engine_binary(entries)
            output = NewDirectory(out)
            for name, info in sorted(entries.items()):
                with package.open(info) as src, output.stream(name) as dst:
                    shutil.copyfileobj(src, dst, 1024 * 1024)
                if os.name != "nt" and ((info.external_attr >> 16) & 0o111):
                    os.chmod(output.path / name, 0o755)
    except zipfile.BadZipFile:
        raise PreparationError("engine_unpack_failed") from None
    return output, output.path / engine_rel


def _install_binary(binary_path, filename, out):
    output = NewDirectory(out)
    with binary_path.open("rb") as src, output.stream(filename) as dst:
        shutil.copyfileobj(src, dst, 1024 * 1024)
    return output, output.path / filename


def _prepare_loaded(
    source,
    out,
    *,
    kind,
    expected_sha256,
    runtime_profile,
    runtime_source,
    runtime_expected_sha256,
    client,
):
    staging = Path(tempfile.mkdtemp(prefix=".engine-prepare-", dir=out.parent))
    try:
        archive_path = staging / "engine" / source.filename
        engine_bytes, engine_sha256 = _download_staged(
            client, source.download_url, archive_path, expected_sha256
        )
        runtime_archive_path = None
        if runtime_source is not None:
            runtime_archive_path = staging / "runtime" / runtime_source.filename
            _download_staged(
                client, runtime_source.download_url, runtime_archive_path, runtime_expected_sha256
            )
        if runtime_profile is not None:
            win_details = windows_runtime_prepare.prepare(
                SimpleNamespace(
                    out=out,
                    runtime_profile=runtime_profile,
                    archive=archive_path,
                    runtime_archive=runtime_archive_path,
                )
            )
            return {
                **win_details,
                "bytes": engine_bytes,
                "sha256": engine_sha256,
                "runtime_receipt": win_details["receipt"],
                "source_record": None,
            }
        if kind == "tar_gz":
            output, engine_path = _extract_tar(archive_path, out)
        elif kind == "zip":
            output, engine_path = _extract_zip(archive_path, out)
        else:
            output, engine_path = _install_binary(archive_path, source.filename, out)
    finally:
        shutil.rmtree(staging, ignore_errors=True)
    if os.name != "nt":
        os.chmod(engine_path, 0o755)
    details = {
        "out": str(output.path),
        "engine": str(engine_path),
        "source_record": str(output.path / "engine-prepare.json"),
        "bytes": engine_bytes,
        "sha256": engine_sha256,
        "runtime_receipt": None,
        "ready_to_run": False,
        "model_requests_sent": 0,
    }
    record = {
        "kind": "engine_prepare.v1",
        "schema_version": 3,
        "definition": DEFINITION,
        "parser": PARSER,
        "source_url": source.url,
        "retrieved_at": datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z"),
        **details,
    }
    output.json("engine-prepare.json", record)
    sync_directory(output.path)
    return details


def prepare(
    url,
    out,
    *,
    expected_sha256=None,
    runtime_profile=None,
    runtime_url=None,
    runtime_expected_sha256=None,
    timeout_seconds=1800,
    client=None,
):
    source = parse_engine_source(url)
    runtime_source = parse_engine_source(runtime_url) if runtime_url is not None else None
    expected_sha256 = validate_sha256(expected_sha256)
    runtime_expected_sha256 = validate_sha256(runtime_expected_sha256)
    if (runtime_url is not None and runtime_profile is None) or (
        runtime_expected_sha256 is not None and runtime_url is None
    ):
        raise PreparationError("invalid_input")
    _timeout(timeout_seconds)
    try:
        out = new_path(out)
    except OSError:
        raise PreparationError("io_error", 4) from None
    kind = None
    if runtime_profile is not None:
        _validate_runtime_profile(
            runtime_profile,
            runtime_source is not None,
            expected_sha256,
            runtime_expected_sha256,
        )
    else:
        kind = _archive_kind(source.filename)
    return run_with_client(
        timeout_seconds,
        client,
        lambda active: _prepare_loaded(
            source,
            out,
            kind=kind,
            expected_sha256=expected_sha256,
            runtime_profile=runtime_profile,
            runtime_source=runtime_source,
            runtime_expected_sha256=runtime_expected_sha256,
            client=active,
        ),
    )
