"""Export a reviewed corpus and deliberately unbound platform examples."""

from inferyard.config.bundle import require_review
from inferyard.config.community_resources import BUNDLES, resource_bytes
from inferyard.config.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    finish,
    new_path,
    require_platform,
    sha256,
)
from inferyard.contracts.validation import ContractError, strict_json_loads

DEFAULT_BUNDLES = ("zh-core", "zh-svg-pelican")


def _load_bundle(name):
    if name not in BUNDLES:
        raise PreparationError("invalid_input")
    raw = resource_bytes(f"bundles/{name}.json")
    try:
        require_review(strict_json_loads(raw.decode()))
    except ValueError, ContractError:
        raise PreparationError("package_resource_invalid", 4) from None
    return raw


def initialize(request):
    require_platform(("Linux", "x64"), ("Windows", "x64"), ("Darwin", "arm64"))
    new_path(request.out)
    names = (request.community_bundle,) if request.community_bundle else DEFAULT_BUNDLES
    bundles = {name: _load_bundle(name) for name in names}
    templates = {
        p: resource_bytes(f"configs/{p}.example.toml") for p in ("linux", "macos", "windows")
    }
    output = NewDirectory(request.out)
    output.write("README.md", resource_bytes("README.md"))
    for name, raw in bundles.items():
        output.write(f"bundles/{name}.json", raw)
    for system, raw in templates.items():
        output.write(f"configs/{system}.example.toml", raw)
    output.directory("assets")
    output.directory("results")
    selected = next(iter(bundles))
    details = {
        **base_details(output.path),
        "bundles": [
            {
                "bundle": name,
                "bundle_path": str(output.path / f"bundles/{name}.json"),
                "bundle_sha256": sha256(raw),
            }
            for name, raw in bundles.items()
        ],
        "bundle": selected,
        "bundle_path": str(output.path / f"bundles/{selected}.json"),
        "bundle_sha256": sha256(bundles[selected]),
        "readme": str(output.path / "README.md"),
        "configs": {p: str(output.path / f"configs/{p}.example.toml") for p in templates},
        "preparation": str(output.path / "preparation.json"),
    }
    return finish(output, "preparation.json", "community_init.v1", details)
