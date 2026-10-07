"""Read checked installation resources identically from editable and wheel packages."""

from importlib.resources import files

from inferyard.config.preparation_io import PreparationError, sha256
from inferyard.contracts.validation import ContractError, strict_json_loads

BUNDLES = ("zh-smoke", "zh-core", "zh-svg-pelican")


def resource_bytes(name):
    root = files("inferyard").joinpath("data/community")
    try:
        manifest = strict_json_loads(root.joinpath("resources.json").read_text(encoding="utf-8"))
        if type(manifest) is not dict or name not in manifest:
            raise ValueError
        raw = root.joinpath(name).read_bytes()
        if sha256(raw) != manifest[name]:
            raise ValueError
        return raw
    except OSError, ValueError, ContractError:
        raise PreparationError("package_resource_invalid", 4) from None
