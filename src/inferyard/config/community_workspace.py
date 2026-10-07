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


def initialize(request):
    require_platform(("Linux", "x64"), ("Windows", "x64"), ("Darwin", "arm64"))
    new_path(request.out)
    name = request.community_bundle
    if name not in BUNDLES:
        raise PreparationError("invalid_input")
    bundle_raw = resource_bytes(f"bundles/{name}.json")
    try:
        bundle = strict_json_loads(bundle_raw.decode())
        require_review(bundle)
    except ValueError, ContractError:
        raise PreparationError("package_resource_invalid", 4) from None
    templates = {
        p: resource_bytes(f"configs/{p}.example.toml") for p in ("linux", "macos", "windows")
    }
    readme = resource_bytes("README.md")
    output = NewDirectory(request.out)
    output.write("README.md", readme)
    output.write(f"bundles/{name}.json", bundle_raw)
    for system, raw in templates.items():
        output.write(f"configs/{system}.example.toml", raw)
    output.directory("assets")
    output.directory("results")
    details = {
        **base_details(output.path),
        "bundle": name,
        "bundle_path": str(output.path / f"bundles/{name}.json"),
        "bundle_sha256": sha256(bundle_raw),
        "readme": str(output.path / "README.md"),
        "configs": {p: str(output.path / f"configs/{p}.example.toml") for p in templates},
        "preparation": str(output.path / "preparation.json"),
    }
    return finish(output, "preparation.json", "community_init.v1", details)
