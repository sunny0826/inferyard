"""Validation reuse never grants trust to new or mutated public inputs."""

import json
from collections import Counter
from copy import deepcopy

import pytest

from inferyard.config.bundle import content_hash, require_review, validate_bundle, validate_case
from inferyard.config.loader import load_config
from inferyard.config.review_cache import command_reviews, review_cache
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts import validation
from inferyard.contracts.schemas import export_schema, schemas_for
from inferyard.contracts.validation import ContractError, Document, validate_document


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


def test_registry_and_wire_specs_are_reused_but_exports_are_detached():
    registry = schemas_for()
    assert schemas_for() is registry
    assert len(registry) == 18
    for kind in registry:
        exported = export_schema(kind)
        original = deepcopy(exported)
        exported.clear()
        assert export_schema(kind) == original
        assert schemas_for()[kind] is registry[kind]


def test_bundle_structure_is_walked_once(monkeypatch, wire_fixture):
    calls = Counter()
    original = validation._validate

    def count(value, spec, path):
        calls[path] += 1
        return original(value, spec, path)

    monkeypatch.setattr(validation, "_validate", count)
    import inferyard.config.bundle as bundles

    monkeypatch.setattr(bundles, "_validate", count)
    for validate in (lambda b: validate_document("bundle", b), validate_bundle):
        calls.clear()
        validate(wire_fixture("bundle"))
        assert calls["bundle"] == 1
        assert calls["bundle.cases[0].case_id"] == 1
        assert calls["case"] == 0


def test_single_plan_reuses_documents_and_rejects_changed_dicts(config_path, monkeypatch):
    loaded = load_config(config_path)
    original = validation.validate_document
    calls = Counter()

    def count(kind, data):
        calls[kind] += 1
        return original(kind, data)

    monkeypatch.setattr(validation, "validate_document", count)
    plan = compile_single_plan(loaded.config, loaded.bundle, experiment_id="reuse")
    assert calls["config"] == calls["bundle"] == 0
    assert plan == compile_single_plan(
        loaded.config.to_dict(), loaded.bundle.to_dict(), experiment_id="reuse"
    )
    assert calls["config"] == calls["bundle"] == 1
    config = loaded.config.to_dict()
    config["execution"]["concurrency"] = True
    with pytest.raises(ContractError):
        compile_single_plan(config, loaded.bundle)
    bundle = loaded.bundle.to_dict()
    bundle["cases"].append(deepcopy(bundle["cases"][0]))
    with pytest.raises(ContractError, match="duplicate case_id"):
        compile_single_plan(loaded.config, bundle)
    plan["request_limit"] += 1
    with pytest.raises(ContractError):
        validate_document("plan", plan)


def test_document_constructor_cannot_forge_trust(wire_fixture):
    bundle = wire_fixture("bundle")
    document = Document("bundle", json.dumps(bundle))
    assert document.to_dict() == bundle
    bundle["schema_version"] = True
    with pytest.raises(ContractError):
        Document("bundle", json.dumps(bundle))
    with pytest.raises(ContractError, match="duplicate JSON key"):
        Document("bundle", '{"schema_version":3,"schema_version":3}')
    with pytest.raises(ContractError):
        compile_single_plan(document, document)


def test_public_case_and_bundle_still_reject_structure_and_semantics(wire_fixture):
    bundle = wire_fixture("bundle")
    case = bundle["cases"][0]
    case["unexpected"] = True
    with pytest.raises(ContractError, match="unknown field"):
        validate_case(case)
    with pytest.raises(ContractError, match="unknown field"):
        validate_bundle(bundle)
    del case["unexpected"]
    case["rules"] = dict(literal_contains=[], literal_forbidden=[], nonempty_line_count=None)
    with pytest.raises(ContractError, match="at least one rule"):
        validate_case(case)
    with pytest.raises(ContractError, match="at least one rule"):
        validate_document("bundle", bundle)


def test_review_cache_is_hash_bound_and_command_local(monkeypatch, wire_fixture):
    import inferyard.config.bundle_review as reviews

    bundle = reviewed_bundle(wire_fixture)
    document = Document.parse("bundle", bundle)
    original = reviews.validate_provenance
    calls = 0

    def count(data):
        nonlocal calls
        calls += 1
        return original(data)

    monkeypatch.setattr(reviews, "validate_provenance", count)
    with command_reviews():
        require_review(document)
        require_review(Document("bundle", document._json))
        # Constructor validates once; the second review reuses the first decision.
        assert calls == 2
        require_review(document)
        assert calls == 2
        bundle["review_records"] = []
        changed = Document.parse("bundle", bundle)
        with pytest.raises(ContractError, match="human_corpus_review_required"):
            require_review(changed)
        with pytest.raises(ContractError, match="human_corpus_review_required"):
            require_review(bundle)
    assert review_cache() is None
    before = calls
    with command_reviews():
        require_review(document)
    assert calls == before + 1
    require_review(document)
    require_review(document)
    assert calls == before + 3


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


def test_cli_review_scope_resets_even_after_failure(monkeypatch):
    import inferyard.cli as cli

    caches = []

    def run(*args, **kwargs):
        caches.append(review_cache())
        assert caches[-1] == set()
        caches[-1].add("fixture")
        raise RuntimeError("fixture")

    monkeypatch.setattr(cli, "run", run)
    for _ in range(2):
        with pytest.raises(RuntimeError, match="fixture"):
            cli.main([])
        assert review_cache() is None
    assert caches[0] is not caches[1]
