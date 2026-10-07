"""Platform-specific file publication, preserving exclusive creation and no-follow opens."""

import hashlib
import os
import re
import stat
from pathlib import Path


def filesystem_path(path):
    """Use extended Windows paths for long files without changing stored names."""
    path = Path(path)
    if os.name == "nt" and len(str(path.absolute())) >= 248:
        from inferyard.platforms.windows_api import _long_path

        return Path(_long_path(path))
    return path


def resolved_within(path, root):
    def plain(value):
        value = str(filesystem_path(value).resolve())
        if value.startswith("\\\\?\\UNC\\"):
            value = "\\\\" + value[8:]
        elif value.startswith("\\\\?\\"):
            value = value[4:]
        return Path(value)

    return plain(path).is_relative_to(plain(root))


def legacy_request_alias(name):
    """A deterministic filename for sealed Linux request snapshots containing ':'."""
    if re.fullmatch(
        r"[A-Za-z0-9][A-Za-z0-9._-]*:(?:probe|warmup|formal):[1-9][0-9]*\.request\.json", name
    ):
        return "request-files/" + hashlib.sha256(name.encode("utf-8")).hexdigest() + ".request.json"
    return None


def open_nofollow(path, flags, mode=0o600):
    if os.name != "nt":
        return os.open(path, flags | os.O_NOFOLLOW, mode)
    # Exclusive creation cannot follow an existing reparse point. For existing
    # files, CreateFileW opens the reparse point itself and checks its attributes.
    from inferyard.platforms.windows_api import open_file

    return open_file(Path(path), flags)


def is_reparse(info):
    return bool(getattr(info, "st_file_attributes", 0) & stat.FILE_ATTRIBUTE_REPARSE_POINT)


def sync_directory(path):
    if os.name == "nt":
        # Windows does not expose POSIX directory fsync. All file contents are
        # fsynced; snapshots use MoveFileExW(WRITE_THROUGH). Do not claim identical
        # metadata crash durability to the Linux directory-fsync implementation.
        return
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def publish(temporary, path, overwrite):
    if os.name == "nt":
        from inferyard.platforms.windows_api import move_file

        move_file(temporary, path, overwrite)
    elif overwrite:
        os.replace(temporary, path)
    else:
        os.link(temporary, path)
        temporary.unlink()


def durability():
    return {
        "file_contents": "fsync",
        "snapshot_publication": "MoveFileExW:WRITE_THROUGH"
        if os.name == "nt"
        else "link_or_replace",
        "directory_fsync": os.name != "nt",
    }
