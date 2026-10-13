"""Offline ZIP member validation shared by installed preparation and legacy wrappers."""

import hashlib
import re
import stat
from pathlib import PurePosixPath, PureWindowsPath

from inferyard.config.preparation_io import PreparationError


def safe_member(name, *, directory=False):
    original = name
    if directory and name.endswith("/"):
        name = name[:-1]
    parts = name.split("/")
    if (
        not name
        or PurePosixPath(name).is_absolute()
        or PureWindowsPath(name).drive
        or any(c in original for c in '\\:\x00<>"|?*')
        or any(ord(c) < 32 for c in original)
        or any(p in ("", ".", "..") or p.endswith((".", " ")) for p in parts)
        or any(
            re.fullmatch(r"(?:CON|PRN|AUX|NUL|COM[1-9¹²³]|LPT[1-9¹²³])(?:\..*)?", p, re.I)
            for p in parts
        )
    ):
        raise PreparationError("unsafe_archive_entry")
    return name


def zip_entries(package):
    entries, names, files = {}, {}, set()
    for entry in package.infolist():
        name = safe_member(entry.orig_filename, directory=entry.is_dir())
        mode = (entry.external_attr >> 16) & 0o170000
        if mode not in (0, stat.S_IFDIR if entry.is_dir() else stat.S_IFREG) or entry.flag_bits & 1:
            raise PreparationError("unsafe_archive_entry")
        folded = name.casefold()
        if folded in names:
            raise PreparationError("unsafe_archive_entry")
        names[folded] = name
        if not entry.is_dir():
            entries[name] = entry
            files.add(folded)
    for folded in names:
        parts = folded.split("/")
        if any("/".join(parts[:i]) in files for i in range(1, len(parts))):
            raise PreparationError("unsafe_archive_entry")
    # Also reject spelling aliases of implicit parent directories.
    parents = {}
    for name in names.values():
        parts = name.split("/")
        for i in range(1, len(parts) + 1):
            prefix = "/".join(parts[:i])
            if parents.setdefault(prefix.casefold(), prefix) != prefix:
                raise PreparationError("unsafe_archive_entry")
    return entries


def member_records(package, role):
    records = []
    for name, entry in sorted(zip_entries(package).items()):
        with package.open(entry) as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        records.append({"role": role, "path": name, "bytes": entry.file_size, "sha256": digest})
    return records
