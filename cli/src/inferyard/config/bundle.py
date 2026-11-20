"""Human review gating for corpus bundles; pure semantics live in the contracts package."""

import hashlib
import json

from inferyard.contracts.review_cache import review_cache
from inferyard.contracts.bundle import validate_bundle
from inferyard.contracts.bundle_review import approved, validate_provenance
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError


def require_review(bundle):
    """Require an approval of current content or a verified equivalent legacy source."""
    cache = review_cache()
    digest = None
    if type(bundle) is Document and bundle.kind == "bundle":
        snapshot = bundle._json.encode()
        if cache is not None:
            digest = hashlib.sha256(snapshot).hexdigest()
            if digest in cache:
                return
        bundle = bundle.to_dict()
        if not (approved(bundle) or any(validate_provenance(bundle))):
            raise ContractError("bundle.review_records", "human_corpus_review_required")
        if digest is not None:
            cache.add(digest)
        return
    else:
        inherited = validate_bundle(bundle)
        if cache is not None:
            digest = hashlib.sha256(
                json.dumps(
                    bundle,
                    ensure_ascii=False,
                    sort_keys=True,
                    separators=(",", ":"),
                    allow_nan=False,
                ).encode()
            ).hexdigest()
            if digest in cache:
                return
    if approved(bundle) or any(inherited):
        if digest is not None:
            cache.add(digest)
        return
    raise ContractError("bundle.review_records", "human_corpus_review_required")
