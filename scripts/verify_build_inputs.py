"""Rehash local conversion inputs against a separately frozen publisher inventory."""

import hashlib
import os
import stat
from pathlib import Path

from inferyard.platforms.platform_io import open_nofollow


def signature(info):
    creation = info.st_birthtime_ns if os.name == "nt" else info.st_ctime_ns
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, creation)


def verify_base_files(plan, root):
    """Require an exact regular-file inventory, then verify bytes and publisher hashes.

    The caller must authenticate the plan hash before calling. This establishes
    local input identity, not conversion execution or comparison eligibility.
    """
    root = Path(root)
    items = plan["files"]
    names = [item["rfilename"] for item in items]
    if (
        not names
        or len(set(names)) != len(names)
        or any(not name or Path(name).name != name or name in (".", "..") for name in names)
    ):
        raise ValueError("invalid_base_inventory")
    if any(type(item["size"]) is not int or item["size"] < 0 for item in items):
        raise ValueError("invalid_base_size")
    if sum(item["size"] for item in items) != plan["declared_total_bytes"]:
        raise ValueError("base_inventory_total_mismatch")
    if {p.name for p in root.iterdir()} != set(names):
        raise ValueError("base_inventory_incomplete_or_unexpected_files")
    rows = []
    for item in items:
        path = root / item["rfilename"]
        before = path.lstat()
        if not stat.S_ISREG(before.st_mode) or before.st_size != item["size"]:
            raise ValueError("base_file_type_or_size_mismatch")
        # Nonblocking protects against a regular path replaced with a FIFO before open.
        fd = open_nofollow(path, os.O_RDONLY | getattr(os, "O_NONBLOCK", 0))
        with os.fdopen(fd, "rb") as stream:
            opened = os.fstat(stream.fileno())
            if not stat.S_ISREG(opened.st_mode) or signature(opened) != signature(before):
                raise ValueError("base_file_changed_before_read")
            sha = hashlib.sha256()
            blob = hashlib.sha1(b"blob " + str(item["size"]).encode() + b"\0")
            count = 0
            while chunk := stream.read(min(4 * 1024**2, item["size"] - count + 1)):
                count += len(chunk)
                if count > item["size"]:
                    raise ValueError("base_file_exceeds_frozen_size")
                sha.update(chunk)
                blob.update(chunk)
            if signature(os.fstat(stream.fileno())) != signature(before) or signature(
                path.lstat()
            ) != signature(before):
                raise ValueError("base_file_changed_during_read")
        if count != item["size"]:
            raise ValueError("base_file_short_read")
        if "lfs" in item:
            matched = item["lfs"]["size"] == count and sha.hexdigest() == item["lfs"]["sha256"]
        else:
            matched = blob.hexdigest() == item["blobId"]
        if not matched:
            raise ValueError("base_file_publisher_hash_mismatch")
        rows.append({"file": item["rfilename"], "bytes": count, "sha256": sha.hexdigest()})
    if {p.name for p in root.iterdir()} != set(names):
        raise ValueError("base_inventory_changed_during_read")
    return rows
