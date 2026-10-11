"""Kind dispatch accepts exactly the original union language and preserves diagnostics."""

from copy import deepcopy

import pytest

from inferyard.contracts import validation
from inferyard.contracts.schemas_common import enum, obj
from inferyard.contracts.schemas_evidence_reference import EVIDENCE_MEMBER
from inferyard.contracts.validation import ContractError


def outcome(validate, value):
    try:
        validation._validate(value, validate, "record.member")
    except ContractError as error:
        return error.path, error.reason
    return None


def validators(spec, monkeypatch):
    optimized = validation._compile(spec, {})
    with monkeypatch.context() as original:
        original.setattr(validation, "_compile_kind_dispatch", lambda *_: None)
        reference = validation._compile(spec, {})
    return optimized, reference


MEMBERS = [
    {"kind": "input", "name": "config", "sha256": "a" * 64},
    {"kind": "observation", "seq": 1, "sha256": "b" * 64},
    {"kind": "request", "request_id": "request-1", "sha256": "c" * 64},
]


@pytest.mark.parametrize("union", ["oneOf", "anyOf"])
@pytest.mark.parametrize("member", MEMBERS)
def test_three_legal_members_match_original_algorithm(monkeypatch, union, member):
    spec = {union: EVIDENCE_MEMBER["oneOf"]}
    fast, original = validators(spec, monkeypatch)
    assert outcome(fast, member) == outcome(original, member) is None


INVALID_MEMBERS = [
    {},
    {"kind": "unknown", "sha256": "a" * 64},
    *[{"kind": value, "name": "run", "sha256": "a" * 64} for value in (True, None, 1, [])],
    {"kind": "input", "name": "unknown", "sha256": "a" * 64},
    {"kind": "input", "sha256": "a" * 64},
    {"kind": "input", "name": "run", "sha256": "a" * 63},
    {"kind": "input", "name": "run", "sha256": "a" * 64, "extra": None},
    {"kind": "observation", "seq": True, "sha256": "a" * 64},
    {"kind": "observation", "seq": float("inf"), "sha256": "a" * 64},
    {"kind": "observation", "seq": 0, "sha256": "a" * 64},
    {"kind": "request", "request_id": None, "sha256": "a" * 64},
    {"kind": "request", "seq": 1, "sha256": "a" * 64},
    None,
    [],
]


@pytest.mark.parametrize("union", ["oneOf", "anyOf"])
@pytest.mark.parametrize("member", INVALID_MEMBERS)
def test_illegal_members_keep_union_error_path_and_reason(monkeypatch, union, member):
    fast, original = validators({union: EVIDENCE_MEMBER["oneOf"]}, monkeypatch)
    assert (
        outcome(fast, member)
        == outcome(original, member)
        == ("record.member", "does not match the allowed types or variants")
    )


def variant(kind, required=True):
    return obj(
        {"kind": enum(kind), "value": {"type": "integer"}},
        ["kind", "value"] if required else ["value"],
    )


@pytest.mark.parametrize("union", ["oneOf", "anyOf"])
@pytest.mark.parametrize("unsafe", ["overlap", "unrequired", "nonstring", "noenum", "nonobject"])
def test_unsafe_kind_variants_use_original_algorithm(monkeypatch, union, unsafe):
    branches = [variant("first"), variant("second")]
    if unsafe == "overlap":
        branches[1]["properties"]["kind"]["enum"].append("first")
    elif unsafe == "unrequired":
        branches[1]["required"].remove("kind")
    elif unsafe == "nonstring":
        branches[1]["properties"]["kind"] = {"type": "boolean", "enum": [True]}
    elif unsafe == "noenum":
        branches[1]["properties"]["kind"] = {"type": "string"}
    else:
        branches[1]["type"] = "array"
    compiled_branches = tuple(validation._compile(branch, {}) for branch in branches)
    assert validation._compile_kind_dispatch(branches, compiled_branches) is None
    fast, original = validators({union: branches}, monkeypatch)
    for value in (
        {"kind": "first", "value": 1},
        {"value": 1},
        {"kind": True, "value": 1},
        {"kind": "other", "value": 1},
        [],
    ):
        assert outcome(fast, value) == outcome(original, value)


@pytest.mark.parametrize("discriminator", ["event_type", "category"])
def test_existing_discriminator_error_order_is_unchanged(monkeypatch, discriminator):
    branches = [
        obj({"kind": enum(kind), discriminator: enum(label), "value": {"type": "integer"}})
        for kind, label in (("first", "a"), ("second", "b"))
    ]
    fast, original = validators({"oneOf": branches}, monkeypatch)
    for value, expected in [
        (
            {"kind": "first", discriminator: "unknown", "value": "wrong"},
            (f"record.member.{discriminator}", "unsupported enum value"),
        ),
        (
            {"kind": "first", discriminator: "a", "value": "wrong"},
            ("record.member.value", "invalid type or non-finite number"),
        ),
    ]:
        assert outcome(fast, value) == outcome(original, value) == expected


@pytest.mark.parametrize("union", ["oneOf", "anyOf"])
def test_nested_union_uses_recursive_validate_and_selected_branch_only(monkeypatch, union):
    spec = obj({"members": {"type": "array", "items": {union: EVIDENCE_MEMBER["oneOf"]}}})
    fast, original = validators(spec, monkeypatch)
    value = {"members": [deepcopy(MEMBERS[-1])]}
    calls = []
    recursive = validation._validate

    def track(item, node, path):
        calls.append(path)
        return recursive(item, node, path)

    monkeypatch.setattr(validation, "_validate", track)
    assert outcome(fast, value) is None
    assert calls.count("record.member.members[0]") == 2  # Union plus selected branch.
    assert "record.member.members[0].request_id" in calls
    calls.clear()
    assert outcome(original, value) is None
    assert calls.count("record.member.members[0]") == 4  # Union plus all three branches.
    value["members"][0]["extra"] = True
    assert (
        outcome(fast, value)
        == outcome(original, value)
        == ("record.member.members[0]", "does not match the allowed types or variants")
    )
