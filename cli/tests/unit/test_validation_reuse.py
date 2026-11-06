"""Cached validation and review never authorize changed public input."""

import json

import pytest

from inferyard.config.bundle import content_hash, require_review
from inferyard.config.loader import load_config
from inferyard.config.review_cache import command_reviews
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.validation import ContractError, Document, validate_document
from inferyard.contracts.validation_cache import command_validation


def reviewed_bundle(wire_fixture):
    bundle = wire_fixture("bundle")
    bundle["review_records"] = [
        dict(
            content_sha256=content_hash(bundle),
            conclusion="approved",
            reviewer="fixture",
            reviewed_at="2026-10-09",
        )
    ]
    return bundle


def test_command_validation_is_content_bound_and_boundary_is_full(wire_fixture):
    bundle = wire_fixture("bundle")
    with command_validation():
        validate_document("bundle", bundle)
        bundle["cases"][0]["unexpected"] = True
        with pytest.raises(ContractError):
            validate_document("bundle", bundle)
        del bundle["cases"][0]["unexpected"]
        bundle["schema_version"] = True
        with pytest.raises(ContractError):
            validate_document("bundle", bundle)


def test_single_plan_reuses_documents_and_rejects_changed_dicts(config_path):
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    config["execution"]["concurrency"] = True
    with pytest.raises(ContractError):
        compile_single_plan(config, loaded.bundle)
    bundle = loaded.bundle.to_dict()
    bundle["cases"].append(bundle["cases"][0].copy())
    with pytest.raises(ContractError, match="duplicate case_id"):
        compile_single_plan(loaded.config, bundle)


def test_document_constructor_cannot_forge_trust(wire_fixture):
    bundle = wire_fixture("bundle")
    bundle["schema_version"] = True
    with pytest.raises(ContractError):
        Document("bundle", json.dumps(bundle))
    with pytest.raises(ContractError, match="duplicate JSON key"):
        Document("bundle", '{"schema_version":3,"schema_version":3}')


def test_review_cache_is_hash_bound_and_command_local(wire_fixture):
    bundle = reviewed_bundle(wire_fixture)
    with command_reviews():
        require_review(Document.parse("bundle", bundle))
        bundle["review_records"] = []
        with pytest.raises(ContractError, match="human_corpus_review_required"):
            require_review(Document.parse("bundle", bundle))
        with pytest.raises(ContractError, match="human_corpus_review_required"):
            require_review(bundle)


def test_cache_does_not_trust_mutable_inputs_or_invalid_proofs(wire_fixture):
    bundle = reviewed_bundle(wire_fixture)
    with command_reviews():
        require_review(Document.parse("bundle", bundle))
        bundle["cases"][0]["unexpected"] = True
        with pytest.raises(ContractError, match="unknown field"):
            require_review(bundle)
        del bundle["cases"][0]["unexpected"]
        bundle["review_provenance"] = [
            dict(source_json="{}", source_sha256="0" * 64, source_content_sha256="0" * 64)
        ]
        with pytest.raises(ContractError):
            require_review(bundle)
