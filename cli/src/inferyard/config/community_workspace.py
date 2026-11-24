"""Export a reviewed corpus and deliberately unbound platform examples."""

import os
import stat
from pathlib import Path

from inferyard.config.bundle import require_review
from inferyard.contracts.bundle import DEFAULT_BUNDLES
from inferyard.platforms.output_tree import anchored
from inferyard.provisioning.community_resources import BUNDLES, resource_bytes
from inferyard.provisioning.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    finish,
    linked,
    require_platform,
    sha256,
)
from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import ContractError

_MANAGED_WORKSPACE_SUBDIRS = frozenset({"models", "engines", "results", "reports"})


def _prepare_workspace_output(raw_out):
    out = Path(raw_out).absolute()
    if not os.path.lexists(out):
        return NewDirectory(out)
    if linked(out) or not stat.S_ISDIR(out.lstat().st_mode):
        raise PreparationError("output_exists")
    for entry in os.scandir(out):
        if (
            entry.name not in _MANAGED_WORKSPACE_SUBDIRS
            or linked(Path(entry.path))
            or not entry.is_dir(follow_symlinks=False)
        ):
            raise PreparationError("output_exists")
    parent = out.parent.resolve(strict=True)
    if not parent.is_dir():
        raise NotADirectoryError(parent)
    resolved = parent / out.name
    output = NewDirectory.__new__(NewDirectory)
    output.path = resolved
    with NewDirectory.errors(), anchored(resolved, create=False) as (tree, handle):
        output.identity = tree.identity(handle)
    return output


def _load_bundle(name):
    if name not in BUNDLES:
        raise PreparationError("invalid_input")
    raw = resource_bytes(f"bundles/{name}.json")
    try:
        require_review(strict_json_loads(raw.decode()))
    except ContractError as error:
        if error.path == "bundle.review_records" and error.reason == "human_corpus_review_required":
            raise PreparationError("human_corpus_review_required", 4) from None
        raise PreparationError("package_resource_invalid", 4) from None
    except ValueError:
        raise PreparationError("package_resource_invalid", 4) from None
    return raw


def initialize(request):
    require_platform(("Linux", "x64"), ("Windows", "x64"), ("Darwin", "arm64"))
    names = (request.community_bundle,) if request.community_bundle else DEFAULT_BUNDLES
    bundles = {name: _load_bundle(name) for name in names}
    templates = {
        p: resource_bytes(f"configs/{p}.example.toml") for p in ("linux", "macos", "windows")
    }
    output = _prepare_workspace_output(request.out)
    roadmap = resource_bytes("README.md").decode().replace("{{WORKSPACE}}", str(output.path))
    output.write("README.md", roadmap.encode())
    for name, raw in bundles.items():
        output.write(f"bundles/{name}.json", raw)
    for system, raw in templates.items():
        output.write(f"configs/{system}.example.toml", raw)
    output.directory("assets")
    output.directory("results")
    for name in ("preflight", "candidates", "bound", "reports"):
        output.directory(name)
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
