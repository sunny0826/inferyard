"""Optional role identities for newly collected runs; legacy fields retain meaning."""

from inferyard.contracts.schemas_common import HASH, LABEL, array, enum, nullable, obj

ROLE_IDENTITY = obj(
    {
        "sha256": HASH,
        "files": array(obj({"path": LABEL, "sha256": HASH}), 1),
        "dependencies": array(
            obj({"name": LABEL, "version": nullable(LABEL), "reason": nullable(LABEL)}), 1
        ),
    }
)
IMPLEMENTATION_IDENTITY = obj(
    {
        "definition": enum("implementation-identity.v1"),
        **{role: ROLE_IDENTITY for role in ("measurement", "scoring", "presentation")},
    }
)
