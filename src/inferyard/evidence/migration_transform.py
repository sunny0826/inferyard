"""Deterministic legacy-to-current transformation; never re-score old answers."""

from __future__ import annotations

from copy import deepcopy

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import validate_document
from inferyard.evidence.migration_source import digest, records, source_documents
from inferyard.evidence.storage import EvidenceError, json_bytes

LIMITATIONS = [
    "format_migration_not_a_new_measurement",
    "original_measurement_and_scorer_identities_preserved",
    "migration_does_not_qualify_current_performance_or_resource_comparison",
]


def _score(score, case):
    category = case["category"]
    result = deepcopy(score)
    result.update(schema_version=SCHEMA_VERSION)
    if "category" not in result:
        result.update(
            category=category,
            scorer_id=f"{category}.legacy-{score['scorer_version']}",
            json_parse_ok=None,
            schema_ok=None,
            field_results=[],
            constraint_results=[],
            expected_label=None,
            predicted_label=None,
        )
        if score["quality_state"] != "unscorable":
            rules = score["rule_results"]
            if category == "instruction":
                result["constraint_results"] = [
                    {"rule": rule["rule"], "passed": rule["passed"]}
                    for rule in rules
                    if rule["rule"] != "nonempty_final_answer"
                ]
            elif category == "extraction":
                by_name = {rule["rule"]: rule for rule in rules}
                parsed_object = by_name.get("json_object", {}).get("passed")
                if parsed_object is None:
                    raise EvidenceError("legacy_extraction_parse_result_missing")
                # Historical json_object cannot distinguish invalid JSON from a
                # valid scalar. Keep that missing distinction explicit.
                result["json_parse_ok"] = True if parsed_object else None
                result["schema_ok"] = score["format_ok"]
                for name in case["rules"]["fields"]:
                    rule = by_name.get("value:" + name)
                    if parsed_object and rule is None:
                        raise EvidenceError("legacy_extraction_field_result_missing")
                    result["field_results"].append(
                        {
                            "path": "/" + name.replace("~", "~0").replace("/", "~1"),
                            "passed": rule["passed"] if rule else False,
                        }
                    )
    validate_document("score", result)
    return result


def _single_plan(documents, config, bundle):
    from inferyard.config.plan_math import plan_hash
    from inferyard.config.single_plan import compile_single_plan

    original = documents["run"]
    old_order = documents["plan"].get("cases")
    expected = [
        {"case_id": case["case_id"], "category": case["category"], "plan_index": index}
        for index, case in enumerate(bundle["cases"])
    ]
    if old_order != expected:
        raise EvidenceError("legacy_single_plan_differs_from_bundle")
    suffix = digest(original["run_id"].encode())[:32]
    plan = compile_single_plan(config, bundle, experiment_id="migrated-" + suffix)
    plan["limitations"].append("experiment_context_reconstructed_after_original_measurement")
    plan["plan_sha256"] = plan_hash(plan)
    trial = plan["trials"][0]
    run = {
        "schema_version": SCHEMA_VERSION,
        "run_id": original["run_id"],
        "experiment_id": plan["experiment"]["experiment_id"],
        "trial_id": trial["trial_id"],
        "plan_sha256": plan["plan_sha256"],
        "parent_run_id": original.get("parent_run_id"),
        "relation": "rerun" if original.get("parent_run_id") else "initial",
        "tool_version": original["tool_version"],
        "tool_source_sha256": original["tool_source_sha256"],
        "definition_versions": plan["experiment"]["definition_versions"],
        "resumed_case_ids": [],
        "diagnostic": original["diagnostic"] or original["kind"] == "check",
        "kind": original["kind"],
        "execution_mode": "single",
        "origin": "migrated",
    }
    # An inferred context must not relabel the measurement/scorer definitions.
    definitions = {key: "legacy-single.v1" for key in run["definition_versions"]}
    plan["experiment"]["definition_versions"] = definitions
    plan["plan_sha256"] = plan_hash(plan)
    run.update(definition_versions=definitions, plan_sha256=plan["plan_sha256"])
    selection = {
        "schema_version": SCHEMA_VERSION,
        "run_id": run["run_id"],
        "trial_id": run["trial_id"],
        "case_ids": trial["case_order"],
        "scorer_sha256": original["scorer_sha256"],
        "parent_events_sha256": None,
    }
    return plan, run, selection


def transform(manifest, blobs):
    """Return only current core files; originals live in the separate receipt archive."""
    from inferyard.config.bundle import upgrade_legacy_bundle
    from inferyard.config.plan_math import plan_hash

    original = source_documents(blobs, manifest)
    old_run = original["run"]
    version = old_run["schema_version"]
    config = deepcopy(original["config.frozen"])
    config["schema_version"] = SCHEMA_VERSION
    bundle = upgrade_legacy_bundle(blobs["bundle.json"].decode("utf-8"))
    validate_document("config", config)
    validate_document("bundle", bundle)
    if version == 1:
        plan, run, selection = _single_plan(original, config, bundle)
    else:
        plan, run, selection = (deepcopy(original[key]) for key in ("plan", "run", "selection"))
        plan["schema_version"] = SCHEMA_VERSION
        plan["experiment"]["schema_version"] = SCHEMA_VERSION
        plan["limitations"].append("legacy_frozen_plan_representation_migrated")
        plan["plan_sha256"] = plan_hash(plan)
        run.update(
            schema_version=SCHEMA_VERSION,
            plan_sha256=plan["plan_sha256"],
            kind="run",
            execution_mode="experiment",
            origin="migrated",
        )
        selection["schema_version"] = SCHEMA_VERSION
    selection.update(
        config_sha256=digest(json_bytes(config)), bundle_sha256=digest(json_bytes(bundle))
    )
    for kind, value in (("plan", plan), ("run", run), ("selection", selection)):
        validate_document(kind, value)
    context = {key: run[key] for key in ("run_id", "experiment_id", "trial_id")}
    cases = {case["case_id"]: case for case in bundle["cases"]}
    request_cases, events = {}, []
    for event in records(blobs["events.jsonl"]):
        if (
            type(event.get("schema_version")) is not int
            or event.get("schema_version") != version
            or event.get("run_id") != old_run["run_id"]
            or version == 2
            and any(event.get(key) != old_run[key] for key in ("experiment_id", "trial_id"))
        ):
            raise EvidenceError("legacy_event_identity_mismatch")
        value = deepcopy(event)
        value.update(schema_version=SCHEMA_VERSION, **context)
        if value["event_type"] == "request_started" and value["phase"] == "formal":
            request_cases[value["request_id"]] = value["data"]["case_id"]
        if value["event_type"] == "score":
            case_id = request_cases.get(value["request_id"])
            if case_id not in cases:
                raise EvidenceError("legacy_score_without_formal_case")
            value["data"] = _score(value["data"], cases[case_id])
        validate_document("event", value)
        events.append(value)
    samples = []
    for sample in records(blobs["memory.jsonl"]):
        if (
            type(sample.get("schema_version")) is not int
            or sample.get("schema_version") != version
            or sample.get("run_id") != old_run["run_id"]
            or version == 2
            and any(sample.get(key) != old_run[key] for key in ("experiment_id", "trial_id"))
        ):
            raise EvidenceError("legacy_sample_identity_mismatch")
        value = {**sample, "schema_version": SCHEMA_VERSION, **context}
        validate_document("sample", value)
        samples.append(value)
    current = {
        name + ".json": json_bytes(value)
        for name, value in (
            ("config.frozen", config),
            ("bundle", bundle),
            ("plan", plan),
            ("run", run),
            ("selection", selection),
        )
    }
    current["events.jsonl"] = b"".join(json_bytes(value) for value in events)
    current["memory.jsonl"] = b"".join(json_bytes(value) for value in samples)
    return current
