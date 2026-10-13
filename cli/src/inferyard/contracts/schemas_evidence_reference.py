"""Physical evidence references, independent of frozen-plan input references."""

from inferyard.contracts.schemas_common import HASH, IDENTIFIER, LABEL, POS, enum, obj

EVIDENCE_MEMBER = {
    "oneOf": [
        obj(
            {
                "kind": enum("input"),
                "name": enum("run", "plan", "selection", "config", "bundle"),
                "sha256": HASH,
            }
        ),
        obj({"kind": enum("observation"), "seq": POS, "sha256": HASH}),
        obj({"kind": enum("request"), "request_id": IDENTIFIER, "sha256": HASH}),
    ]
}
EVIDENCE_REFERENCE = obj(
    {"path": LABEL, "sha256": HASH, "member": EVIDENCE_MEMBER}, ["path", "sha256"]
)
