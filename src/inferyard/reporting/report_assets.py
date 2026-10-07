"""Versioned installed report templates and their complete content identity."""

import hashlib
from importlib.resources import files

_SNAPSHOTS = {2: "v2", 3: "v3", 4: "v4", 5: "v5"}


def template_name(version):
    if version == 1:
        return "report_v1.html"
    if version in _SNAPSHOTS:
        return _SNAPSHOTS[version] + "/report.html"
    return "report.html"


def template_hash(version):
    root = files("inferyard").joinpath("templates")
    if version == 1:
        return hashlib.sha256(root.joinpath("report_v1.html").read_bytes()).hexdigest()
    if version in _SNAPSHOTS:
        root = root.joinpath(_SNAPSHOTS[version])
    digest = hashlib.sha256()
    for path in sorted(root.iterdir(), key=lambda p: p.name):
        if (
            path.name.startswith("report")
            and path.name.endswith(".html")
            and path.name != "report_v1.html"
        ):
            digest.update(path.name.encode() + b"\0")
            digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()
