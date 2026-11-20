"""Read packaged community resources directly from editable and wheel packages."""

from importlib.resources import files
from pathlib import PurePosixPath

from inferyard.provisioning.preparation_io import PreparationError

BUNDLES = ("zh-smoke", "zh-core", "zh-svg-pelican", "zh-context-speed")


def resource_bytes(name):
    if not isinstance(name, str) or not name or "\\" in name or "\x00" in name:
        raise PreparationError("package_resource_invalid", 4)
    rel = PurePosixPath(name)
    if (
        rel.is_absolute()
        or not rel.parts
        or any(part in ("", ".", "..") or ":" in part for part in rel.parts)
        or rel.as_posix() != name
    ):
        raise PreparationError("package_resource_invalid", 4)
    root = files("inferyard").joinpath("data/community")
    try:
        return root.joinpath(*rel.parts).read_bytes()
    except (OSError, ValueError):
        raise PreparationError("package_resource_invalid", 4) from None
