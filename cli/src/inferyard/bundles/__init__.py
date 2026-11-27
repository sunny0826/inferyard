"""Dynamic benchmark bundle discovery, metadata resolution, and verification."""

from inferyard.bundles.registry import (
    bundle_bytes,
    bundle_names,
    catalog,
    catalog_entries,
    catalog_entry,
    default_bundles,
    effective_execution_hints,
    effective_metadata,
    is_context_speed_bundle,
    resolve_bundle_spec,
    verify_bundles,
)

__all__ = [
    "bundle_bytes",
    "bundle_names",
    "catalog",
    "catalog_entries",
    "catalog_entry",
    "default_bundles",
    "effective_execution_hints",
    "effective_metadata",
    "is_context_speed_bundle",
    "resolve_bundle_spec",
    "verify_bundles",
]
