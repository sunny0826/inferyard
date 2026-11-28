"""Read packaged community resources directly from editable and wheel packages."""

from importlib.resources import files
from pathlib import PurePosixPath

from inferyard.bundles.registry import bundle_bytes, bundle_names
from inferyard.provisioning.preparation_io import PreparationError

BUNDLES = bundle_names()


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
    if len(rel.parts) == 2 and rel.parts[0] == "bundles" and rel.parts[1].endswith(".json"):
        return bundle_bytes(rel.parts[1].removesuffix(".json"))
    root = files("inferyard").joinpath("data/community")
    try:
        return root.joinpath(*rel.parts).read_bytes()
    except (OSError, ValueError):
        raise PreparationError("package_resource_invalid", 4) from None
