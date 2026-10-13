"""Purpose-specific B contracts; synthetic observations only."""

from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.config.bundle import content_hash, require_review
from inferyard.config.bundle_review import approved_cases, case_content_hash
from inferyard.config.environment_binding import admission
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError
from inferyard.reporting.comparison_report import (
    build_comparison,
    comparison_input,
    read_verified_comparison,
)
from inferyard.runtime.service_reuse import require_transition
from tests.helpers import fixture_run


@pytest.fixture
def pair(tmp_path):
    return [comparison_input(fixture_run(tmp_path / side))[0] for side in ("a", "b")]


def test_side_by_side_observed_quality_with_device_and_engine_differences(pair):
    a, b = pair
    b["config"]["device"]["id"] = "different"
    b["config"]["engine"]["release"] = "different"
    result = compare_trials(a, b, definition="phase2.v3")
    assert not result["eligibility"]["quality"]
    observed = result["observed_differences"]
    assert observed["completion_rate"]["difference"] == 0
    assert any(row["difference"] == 0 for row in observed["quality"])
    assert observed["condition_differences"]
    assert observed["performance"]
    assert all(row["difference"] is None for row in observed["performance"])
    assert len(observed["per_case"]) == 3


@pytest.mark.parametrize(
    "change", ["prompt", "rules", "answer_policy", "scorer", "denominator", "incomplete"]
)
def test_quality_overall_null_when_content_or_scope_does_not_match(pair, change):
    a, b = pair
    if change == "prompt":
        b["bundle"]["cases"][0]["prompt"] += "changed"
    elif change == "rules":
        b["bundle"]["cases"][0]["rules"]["literal_contains"].append("changed")
    elif change == "answer_policy":
        b["bundle"]["answer_policy"]["strip_line_edges"] = False
    elif change == "scorer":
        b["selection"]["scorer_sha256"] = "b" * 64
    elif change == "denominator":
        b["summary"]["counts"]["valid_executed"] -= 1
    else:
        b["summary"]["completeness"] = "incomplete"
    observed = compare_trials(a, b, definition="phase2.v3")["observed_differences"]
    assert all(row["difference"] is None for row in observed["quality"])
    assert observed["per_case"]


def test_only_offline_v2_ignores_legacy_comparison_marker(pair):
    a, b = pair
    b["run"]["definition_versions"]["comparison"] = "legacy-marker"
    new = compare_trials(a, b, definition="phase2.v3")
    assert all("comparison" not in c["field"] for c in new["conditions"])
    b["run"]["definition_versions"]["measurement"] = "different"
    changed = compare_trials(a, b, definition="phase2.v3")["observed_differences"]
    assert changed["completion_rate"]["difference"] is None


@pytest.mark.parametrize("version", [1, 2, 3])
def test_old_comparison_rejected_before_source_loading(tmp_path, version):
    from inferyard.evidence.formats import UnsupportedFormat

    (tmp_path / "comparison.json").write_bytes(
        json_bytes({"schema_version": 3, "format_version": version})
    )
    with pytest.raises(UnsupportedFormat):
        read_verified_comparison(tmp_path)
    with pytest.raises(UnsupportedFormat):
        build_comparison(tmp_path / "missing", tmp_path / "missing2", format_version=version)


@pytest.mark.parametrize("diagnostic", [False, True])
@pytest.mark.parametrize("policy", [None, [], ["ac_online"], ["epp"]])
def test_environment_admission_legacy_and_explicit(diagnostic, policy):
    conditions = dict(
        ac_online=True,
        profile="performance",
        governor="performance",
        epp="performance",
        require_epp_match=False,
    )
    environment = dict(platform="Linux", ac_online=False, profile=None, governor=None, epp=None)
    declaration = (
        None
        if policy is None
        else {"definition": "environment-admission.v2", "required_fields": policy}
    )
    result = admission(conditions, environment, diagnostic=diagnostic, policy=declaration)
    assert result["differences"]
    assert result["blockers"] == (
        []
        if diagnostic or policy == []
        else policy
        if policy is not None
        else ["ac_online", "profile", "governor"]
    )


def test_case_review_only_invalidates_affected_content(pair):
    bundle = deepcopy(pair[0]["bundle"])
    bundle["review_records"] = []
    bundle["case_review_records"] = [
        dict(
            definition="case-review.v1",
            case_id=c["case_id"],
            content_sha256=case_content_hash(bundle, c),
            reviewer="human",
            reviewed_at="2026-10-07",
            conclusion="approved",
        )
        for c in bundle["cases"]
    ]
    require_review(bundle)
    bundle["license_note"] = "new attribution"
    bundle["version"] = "display-version"
    require_review(bundle)
    original = deepcopy(bundle)
    bundle["cases"][0]["prompt"] += " new"
    assert approved_cases(bundle) == {c["case_id"] for c in bundle["cases"][1:]}
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)
    bundle["case_review_records"].append(
        {
            **bundle["case_review_records"][0],
            "content_sha256": case_content_hash(bundle, bundle["cases"][0]),
        }
    )
    require_review(bundle)
    bundle["answer_policy"]["strip_line_edges"] = not bundle["answer_policy"]["strip_line_edges"]
    assert approved_cases(bundle) == set()
    original["case_review_records"][0]["extra"] = True
    with pytest.raises(ContractError):
        Document.parse("bundle", original)


def test_legacy_bundle_approval_unchanged(pair):
    bundle = pair[0]["bundle"]
    bundle["review_records"] = [
        dict(
            reviewer="old human",
            reviewed_at="before",
            conclusion="approved",
            content_sha256=content_hash(bundle),
        )
    ]
    require_review(bundle)
    bundle["cases"][0]["reference_answer"] += "new"
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)


@pytest.mark.parametrize(
    "changed", ["model", "engine", "generation", "context_size", "fresh", None]
)
@pytest.mark.parametrize("serial_continuation", [False, True])
def test_process_reuse_requires_matching_clean_scope(pair, changed, serial_continuation):
    previous = pair[0]
    previous["service_drain"] = {"completion_scope": "engine_idle"}
    config = deepcopy(previous["config"])
    if changed in ("model", "engine", "generation"):
        config[changed]["changed"] = True
    elif changed == "context_size":
        config["conditions"]["context_size"] += 1
    elif changed == "fresh":
        config["execution"]["require_fresh_process"] = True

    def never_query(_):
        pytest.fail("same service must not be required to exit")

    if changed:
        with pytest.raises(PreflightError, match="fresh_required"):
            require_transition(
                previous,
                config,
                never_query,
                reason="fresh_required",
                serial_continuation=serial_continuation,
            )
    else:
        assert (
            require_transition(
                previous,
                config,
                never_query,
                reason="fresh_required",
                serial_continuation=serial_continuation,
            )
            == "same_process"
        )


def test_replacement_requires_old_process_exit_and_propagates_unknown(pair):
    previous = pair[0]
    config = deepcopy(previous["config"])
    config["endpoint"]["server_pid"] += 1
    with pytest.raises(PreflightError, match="previous_service_still_alive"):
        require_transition(
            previous,
            config,
            lambda _: previous["config"]["endpoint"]["process_start_ticks"],
            reason="fresh",
        )

    def gone(_):
        raise PreflightError("service_process_unavailable")

    assert require_transition(previous, config, gone, reason="fresh") == "replaced_process"

    def unknown(_):
        raise PreflightError("process_inspection_denied")

    with pytest.raises(PreflightError, match="process_inspection_denied"):
        require_transition(previous, config, unknown, reason="fresh")


def test_mismatched_legacy_approval_is_not_projected_to_cases(pair):
    bundle = deepcopy(pair[0]["bundle"])
    bundle["review_records"] = [
        dict(
            reviewer="human",
            reviewed_at="before",
            conclusion="approved",
            content_sha256=content_hash(bundle),
        )
    ]
    original_hash = content_hash(bundle)
    require_review(bundle)
    bundle["license_note"] += " display edit"
    assert content_hash(bundle) != original_hash
    assert approved_cases(bundle) == set()
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)
    # One new approval cannot implicitly approve the other unchanged cases.
    case = bundle["cases"][0]
    bundle["case_review_records"] = [
        dict(
            definition="case-review.v1",
            case_id=case["case_id"],
            content_sha256=case_content_hash(bundle, case),
            reviewer="human",
            reviewed_at="after",
            conclusion="approved",
        )
    ]
    assert approved_cases(bundle) == {case["case_id"]}
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)


def test_whole_bundle_hash_still_excludes_only_old_review_records(pair):
    import hashlib
    import json

    bundle = pair[0]["bundle"]
    bundle["case_review_records"] = []
    expected = hashlib.sha256(
        json.dumps(
            {k: v for k, v in bundle.items() if k != "review_records"},
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode()
    ).hexdigest()
    assert content_hash(bundle) == expected
    bundle.pop("case_review_records")
    assert content_hash(bundle) != expected


def test_v2_definition_checks_apply_to_their_metric_family(pair):
    a, b = pair
    for d in pair:
        d["plan"]["experiment"]["comparison"] = {"mode": "model", "factor": None}
    b["run"]["definition_versions"]["scoring"] = "different"
    result = compare_trials(a, b, definition="phase2.v3")
    assert not result["eligibility"]["quality"]
    assert result["eligibility"]["completion"]
    assert not any("scoring" in reason for reason in result["blockers"])
    b["run"]["definition_versions"]["scoring"] = a["run"]["definition_versions"]["scoring"]
    b["run"]["definition_versions"]["measurement"] = "different"
    result = compare_trials(a, b, definition="phase2.v3")
    assert result["eligibility"]["quality"]
    assert not result["eligibility"]["completion"]
    assert any(
        c["impact"] == "performance" and c["status"] == "different"
        for c in result["conditions"]
        if c["field"].endswith("measurement")
    )


@pytest.mark.parametrize(
    "policy",
    [
        {"definition": "environment-admission.v1", "required_fields": []},
        {"definition": "environment-admission.v2", "required_fields": ["ac_online", "ac_online"]},
        {"definition": "environment-admission.v2", "required_fields": [True]},
        {"definition": "environment-admission.v2", "required_fields": ["temperature"]},
    ],
)
def test_environment_policy_rejects_ambiguous_or_safety_fields(pair, policy):
    config = pair[0]["config"]
    config["conditions"]["environment_admission"] = policy
    with pytest.raises(ContractError):
        Document.parse("config", config)


def test_clean_incomplete_source_is_not_a_restart_gate(pair):
    previous = pair[0]
    previous["service_drain"] = {"completion_scope": "engine_idle"}
    previous["summary"]["scope_complete"] = False
    previous["summary"]["completeness"] = "incomplete"
    assert (
        require_transition(
            previous,
            previous["config"],
            lambda _: pytest.fail("same process exit query"),
            reason="fresh",
        )
        == "same_process"
    )


@pytest.mark.parametrize("engine", ["prism", "kvmem", "ninfer"])
def test_same_process_without_drain_proof_is_explicitly_refused(pair, engine):
    previous = pair[0]
    previous["config"]["engine"]["adapter"] = engine
    with pytest.raises(PreflightError, match="service_reuse_drain_evidence_missing"):
        require_transition(previous, previous["config"], lambda _: None, reason="fresh")


def test_plan_environment_policy_overrides_config_without_rewriting_conditions(pair):
    conditions = pair[0]["config"]["conditions"]
    conditions["environment_admission"] = {
        "definition": "environment-admission.v2",
        "required_fields": ["ac_online"],
    }
    original = deepcopy(conditions)
    observed = {"platform": "Linux", "ac_online": None}
    configured = admission(conditions, observed)
    assert configured["blockers"] == ["ac_online"]
    assert configured["policy_source"] == "config"
    override = {"definition": "environment-admission.v2", "required_fields": []}
    planned = admission(conditions, observed, policy=override)
    assert planned["blockers"] == []
    assert planned["policy"] == override
    assert planned["policy_source"] == "plan"
    assert conditions == original


@pytest.mark.parametrize(
    "field",
    [
        "case_id",
        "prompt",
        "reference_answer",
        "rules",
        "category",
        "task_protocol",
        "answer_policy",
    ],
)
def test_case_hash_covers_every_supported_semantic_field(pair, field):
    bundle = deepcopy(pair[0]["bundle"])
    case = bundle["cases"][0]
    before = case_content_hash(bundle, case)
    if field in ("task_protocol", "answer_policy"):
        bundle[field] = "changed" if field == "task_protocol" else {"changed": True}
    else:
        case[field] = {"changed": True} if field == "rules" else "changed"
    assert case_content_hash(bundle, case) != before
