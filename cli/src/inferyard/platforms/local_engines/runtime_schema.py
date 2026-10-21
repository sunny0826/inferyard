"""Closed runtime snapshot data shape; inventories are verified separately."""

from inferyard.contracts.schemas_common import (
    HASH,
    LABEL,
    NAT,
    POS,
    VERSION,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.contracts.schemas_local_engines import INSTALLATION

PYTHON = obj(
    {
        "path": array(LABEL),
        "prefix": LABEL,
        "base_prefix": LABEL,
        "paths": obj(
            dict.fromkeys(
                (
                    "stdlib",
                    "platstdlib",
                    "purelib",
                    "platlib",
                    "include",
                    "platinclude",
                    "scripts",
                    "data",
                ),
                LABEL,
            )
        ),
        "version": {"type": "array", "items": POS, "minItems": 2, "maxItems": 2},
        "site_packages": LABEL,
        "import_roots": array(LABEL),
        "entrypoint_sources": array(LABEL, 1),
    }
)
ENTRY = obj({"path": LABEL, "sha256": HASH, "size": NAT, "symlink": nullable(LABEL)})
RUNTIME = obj(
    {
        "schema_version": VERSION,
        "definition": enum("local-engine-runtime.v1"),
        "installation": INSTALLATION,
        "python": nullable(PYTHON),
        "unresolved_libraries": array(
            obj(
                {
                    "path": LABEL,
                    "dependency": LABEL,
                    "reason": enum("unresolved", "ambiguous_or_outside_inventory"),
                }
            )
        ),
        "roots": array(obj({"path": LABEL, "entries": array(ENTRY)}), 1),
    }
)
