"""F01 contract slice: independently specified valid and invalid wire documents."""

from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard.config.loader import load_config
from inferyard.contracts.schemas import export_schema, schemas_for
from inferyard.contracts.validation import (
    ContractError,
    Document,
    strict_json_loads,
    validate_document,
)


@pytest.mark.parametrize("kind", ["bundle", "event", "sample", "summary"])
def test_valid_wire_contracts_and_independent_json_schema(kind, wire_fixture):
    data = wire_fixture(kind)
    schema = export_schema(kind)
    Draft202012Validator.check_schema(schema)
    Draft202012Validator(schema).validate(data)
    document = Document.parse(kind, data)
    assert document.to_dict() == data
    data["schema_version"] = 999
    assert document.to_dict()["schema_version"] == 3
    output = document.to_dict()
    output["schema_version"] = 999
    assert document.to_dict()["schema_version"] == 3


def test_all_published_schemas_are_valid():
    for kind in schemas_for():
        Draft202012Validator.check_schema(export_schema(kind))


def test_config_schema_matches_normalized_input(config_path):
    config = load_config(config_path).config.to_dict()
    Draft202012Validator(export_schema("config")).validate(config)


def test_explicit_device_memory_budget_keeps_default_and_survives_loading(config_path, tmp_path):
    import json

    from inferyard.contracts.schemas import CONFIG_DEFAULTS

    config = load_config(config_path).config.to_dict()
    assert CONFIG_DEFAULTS["output"]["min_available_memory_bytes"] == 8 * 1024**3
    config["output"]["min_available_memory_bytes"] = 1024**3
    for kind in ("config_input", "config"):
        Draft202012Validator(export_schema(kind)).validate(config)
        assert Document.parse(kind, config).to_dict() == config
    candidate = tmp_path / "explicit-budget.toml"
    lines = ["schema_version = 3"]
    for section, values in config.items():
        if isinstance(values, dict):
            lines.append(f"[{section}]")
            lines.extend(f"{key} = {json.dumps(value)}" for key, value in values.items())
    candidate.write_text("\n".join(lines) + "\n", encoding="utf-8")
    assert (
        load_config(candidate).config.to_dict()["output"]["min_available_memory_bytes"] == 1024**3
    )


@pytest.mark.parametrize("budget", [0, -1, True, 1024**3 - 1])
def test_device_memory_budget_rejects_values_below_safety_floor(config_path, budget):
    config = load_config(config_path).config.to_dict()
    config["output"]["min_available_memory_bytes"] = budget
    for kind in ("config_input", "config"):
        assert not Draft202012Validator(export_schema(kind)).is_valid(config)
        with pytest.raises(ContractError, match="min_available_memory_bytes"):
            Document.parse(kind, config)


@pytest.mark.parametrize("kind", ["bundle", "event", "sample", "summary"])
@pytest.mark.parametrize("change", ["unknown", "wrong_version", "bool_version"])
def test_structural_errors_rejected_by_both_validators(kind, change, wire_fixture):
    data = wire_fixture(kind)
    if change == "unknown":
        data["unrecognized"] = "not allowed"
    else:
        data["schema_version"] = True if change == "bool_version" else 2
    assert not Draft202012Validator(export_schema(kind)).is_valid(data)
    with pytest.raises(ContractError):
        validate_document(kind, data)


@pytest.mark.parametrize(
    "changes,path",
    [
        ({"execution_state": "success"}, "event.data.execution_state"),
        ({"protocol_complete": False}, "event.data.execution_state"),
        ({"raw_finish_reason": "tool_calls"}, "event.data.execution_state"),
        ({"t_terminal_ns": -1}, "event.data.t_terminal_ns"),
        ({"t_first_content_ns": 1300000000}, "event.data.t_first_content_ns"),
        ({"t_first_answer_ns": 100000000}, "event.data.t_first_answer_ns"),
        ({"budget_exhausted": True}, "event.data.budget_exhausted"),
        ({"completion_tokens": True}, "event.data.completion_tokens"),
        ({"execution_state": "failed"}, "event.data.error_category"),
    ],
)
def test_event_state_and_time_invariants(changes, path, wire_fixture):
    data = wire_fixture("event")
    data["data"].update(changes)
    with pytest.raises(ContractError) as error:
        validate_document("event", data)
    assert error.value.path == path


def test_unknown_event_type_is_not_silently_accepted(wire_fixture):
    event = wire_fixture("event")
    event["event_type"] = "everything_was_ok"
    with pytest.raises(ContractError, match="event.event_type"):
        validate_document("event", event)


@pytest.mark.parametrize(
    "changes",
    [
        {"value": None},
        {"value": 0, "missing_reason": "unavailable"},
        {"server_pid": None},
        {"process_start_ticks": None},
        {"read_finished_ns": 1},
        {"unit": "GiB"},
    ],
)
def test_sample_invalid_missingness_identity_and_window(changes, wire_fixture):
    sample = wire_fixture("sample")
    sample.update(changes)
    with pytest.raises(ContractError):
        validate_document("sample", sample)


def test_sample_missing_and_zero_are_distinct(wire_fixture):
    sample = wire_fixture("sample")
    sample["value"] = 0
    validate_document("sample", sample)
    sample.update(value=None, missing_reason="source_changed")
    validate_document("sample", sample)


@pytest.mark.parametrize(
    "target,value",
    [
        ("planned", 8),
        ("executed", 6),
        ("valid_executed", 6),
        ("budget_exhausted", 8),
        ("completed", True),
    ],
)
def test_summary_ledger_rejects_inconsistent_counts(target, value, wire_fixture):
    summary = wire_fixture("summary")
    summary["counts"][target] = value
    with pytest.raises(ContractError):
        validate_document("summary", summary)


def test_summary_does_not_allow_incomplete_quality_score(wire_fixture):
    summary = wire_fixture("summary")
    summary["completeness"] = "incomplete"
    with pytest.raises(ContractError, match="incomplete quality"):
        validate_document("summary", summary)


def test_summary_does_not_silently_drop_unscorable_case(wire_fixture):
    summary = wire_fixture("summary")
    quality = summary["quality"]["Q01"]["instruction"]
    quality["passed"] = 1
    quality["unscorable"] = 1
    quality["rate"].update(numerator=1, denominator=6, value=1 / 6)
    with pytest.raises(ContractError):
        validate_document("summary", summary)


def test_summary_rejects_fabricated_empty_distribution(wire_fixture):
    summary = wire_fixture("summary")
    summary["performance"]["instruction"]["metrics"]["L06"]["max"] = 0
    with pytest.raises(ContractError, match="empty distribution"):
        validate_document("summary", summary)


@pytest.mark.parametrize("value", [float("nan"), float("inf"), -float("inf"), True])
def test_non_finite_or_boolean_measurements_rejected(value, wire_fixture):
    summary = wire_fixture("summary")
    summary["performance"]["instruction"]["metrics"]["L03"]["p50"] = value
    with pytest.raises(ContractError):
        validate_document("summary", summary)


def test_bundle_duplicate_id_and_typed_references(wire_fixture):
    bundle = wire_fixture("bundle")
    duplicate = deepcopy(bundle)
    duplicate["cases"].append(deepcopy(bundle["cases"][0]))
    with pytest.raises(ContractError, match="duplicate case_id"):
        validate_document("bundle", duplicate)
    bundle["cases"][1]["rules"]["fields"]["age"]["value"] = True
    with pytest.raises(ContractError, match="reference value"):
        validate_document("bundle", bundle)


@pytest.mark.parametrize(
    "raw",
    [
        '{"a":1,"a":2}',
        '{"a":NaN}',
        '{"a":Infinity}',
        '{"a":-Infinity}',
        '{"a":"fixture-secret-7f39"',
    ],
)
def test_strict_json_errors_do_not_echo_content(raw):
    with pytest.raises(ContractError) as error:
        strict_json_loads(raw)
    assert "fixture-secret-7f39" not in str(error.value)


@pytest.mark.parametrize("metric,unit", [("L03", "bytes"), ("L04", "ns")])
def test_summary_rejects_wrong_metric_units(metric, unit, wire_fixture):
    summary = wire_fixture("summary")
    summary["performance"]["instruction"]["metrics"][metric]["unit"] = unit
    with pytest.raises(ContractError, match="unsupported enum"):
        validate_document("summary", summary)


def test_memory_evidence_keeps_byte_unit(wire_fixture):
    sample = wire_fixture("sample")
    sample["unit"] = "ns"
    with pytest.raises(ContractError):
        validate_document("sample", sample)


def test_summary_incomplete_cannot_claim_comparison_eligibility(wire_fixture):
    summary = wire_fixture("summary")
    assert any(m["comparison_eligible"] for m in summary["metric_observations"])
    summary["completeness"] = "incomplete"
    for group in summary["quality"]["Q01"].values():
        group["rate"].update(value=None, reason="incomplete_run")
    for code in ("Q02", "Q03", "Q04", "Q05", "Q06", "Q07"):
        summary["quality"][code].update(value=None, reason="incomplete_run")
    with pytest.raises(ContractError, match="incomplete trial cannot qualify metrics"):
        validate_document("summary", summary)
