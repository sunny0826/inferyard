"""Versioned installed report templates and their complete content identity."""

import hashlib
from importlib.resources import files

from inferyard.evidence.formats import require_version


def template_name(version):
    require_version({"version": version}, "version", (8,), "report")
    return "report.html"


def template_hash(version):
    template_name(version)
    root = files("inferyard").joinpath("templates")
    digest = hashlib.sha256()
    for path in sorted(root.iterdir(), key=lambda p: p.name):
        if path.name.startswith("report") and path.name.endswith(".html"):
            digest.update(path.name.encode() + b"\0")
            digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()
