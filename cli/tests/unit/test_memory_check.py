"""User-visible grouping, conservative decisions and saved diagnostic rejection paths."""

from copy import deepcopy

import pytest

from inferyard.application.memory_check import inspect_memory
from inferyard.contracts.memory_check import read_memory_check, validate_memory_check
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.memory_macos import reading


def observed(pid, *, parent=0, bundle=None, executable="/opt/tool", uid=501, missing=None):
    return {
        "pid": pid,
        "start_identity": pid * 100 if not missing else None,
        "parent_pid": parent,
        "parent_start_identity": parent * 100 if parent and not missing else None,
        "name": "worker",
        "uid": uid,
        "executable": executable,
        "bundle": bundle,
        "rss": reading(None if missing else pid * 1024, "fixture:rss", missing),
        "missing_reason": missing,
    }


def app(name="Browser", key="first"):
    return {"name": name, "key": key, "path": f"/Applications/{name}.app"}


def snapshot(rows, *, caller=999, truncation=None, total=None):
    return {
        "rows": rows,
        "caller_pid": caller,
        "total_pids": len(rows) if total is None else total,
        "memory_total": reading(16000, "fixture:total"),
        "memory_available": reading(5000, "fixture:available"),
        "truncation_reason": truncation,
        "collection_ns": 123,
    }


def report(rows, *, protected=(), **options):
    value = snapshot(rows, **options)
    return inspect_memory(protected, collector=lambda **_: value, uid=501)


def by_pid(value, pid):
    return next(p for p in value["processes"] if p["pid"] == pid)


def test_review_groups_helpers_and_multiple_instances_without_merging_install_copies():
    data = report(
        [
            observed(10, bundle=app()),
            observed(11, parent=10),
            observed(12, bundle=app()),
            observed(13, bundle=app(key="copy")),
        ]
    )
    assert data["coverage"]["complete"]
    first = next(g for g in data["groups"] if 10 in g["member_pids"])
    assert first["member_pids"] == [10, 11, 12]
    assert first["root_pids"] == [10, 12]
    assert first["rss_total_bytes"] == 33 * 1024
    assert first["decision"]["status"] == "review"
    assert len(data["groups"]) == 2
    assert by_pid(data, 13)["group_id"] != by_pid(data, 10)["group_id"]


def test_declared_protection_reaches_descendants_and_entire_app_not_siblings():
    data = report(
        [
            observed(10, bundle=app()),
            observed(11, parent=10),
            observed(12, bundle=app()),
            observed(13),
        ],
        protected=[10, 10],
    )
    assert data["protected_requests"] == [
        {"pid": 10, "start_identity": 1000, "status": "verified", "reason": None}
    ]
    assert by_pid(data, 11)["decision"]["status"] == "protected"
    assert data["groups"][0]["decision"]["status"] == "protected"
    assert by_pid(data, 13)["decision"]["status"] == "unknown"


def test_unknown_interpreter_and_orphan_helper_remain_unknown():
    data = report([observed(10), observed(11, parent=1), observed(12, bundle=app())])
    assert by_pid(data, 10)["decision"]["status"] == "unknown"
    assert by_pid(data, 11)["group_id"] is None
    assert by_pid(data, 12)["decision"]["status"] == "review"


def test_caller_ancestors_protected_without_protecting_unrelated_siblings():
    data = report(
        [
            observed(10, bundle=app()),
            observed(11, parent=10),
            observed(12, parent=10, bundle=app("Other", "other")),
        ],
        caller=11,
    )
    assert by_pid(data, 10)["decision"]["status"] == "protected"
    assert by_pid(data, 11)["decision"]["status"] == "protected"
    assert by_pid(data, 12)["decision"]["status"] != "protected"


@pytest.mark.parametrize(
    "row",
    [
        observed(10, executable="/home/user/.lody/agent-binaries/codex"),
        observed(10, bundle=app("Cursor")),
        observed(10, executable="/usr/libexec/service"),
        observed(10, uid=0),
    ],
)
def test_known_agents_services_connections_and_other_accounts_are_protected(row):
    data = report([row, observed(11, parent=10)])
    assert by_pid(data, 10)["decision"]["status"] == "protected"


def test_parent_identity_change_does_not_bind_to_new_pid_generation():
    child = observed(11, parent=10)
    child["parent_start_identity"] = 9999
    data = report([observed(10, bundle=app()), child], protected=[10])
    assert by_pid(data, 11)["decision"]["status"] == "unknown"
    assert by_pid(data, 11)["group_id"] is None


def test_conflicting_bundle_and_parent_evidence_does_not_merge_apps():
    data = report(
        [
            observed(10, bundle=app()),
            observed(11, parent=10, bundle=app("Other", "other")),
        ]
    )
    assert by_pid(data, 11)["group_id"] is None
    assert by_pid(data, 11)["decision"]["status"] == "unknown"


@pytest.mark.parametrize(
    "options",
    [
        {"protected": [404]},
        {"truncation": "deadline", "total": 20},
        {"truncation": "process_limit", "total": 20},
    ],
)
def test_unavailable_protection_or_incomplete_scan_never_produces_review(options):
    data = report([observed(10, bundle=app())], **options)
    assert not data["coverage"]["complete"]
    assert by_pid(data, 10)["decision"]["status"] == "unknown"


def test_access_denied_is_null_and_blocks_review_when_same_account_may_be_unseen():
    data = report([observed(10, bundle=app()), observed(11, missing="access_denied")])
    assert data["coverage"]["missing"] == 1
    assert by_pid(data, 11)["rss"]["value"] is None
    assert by_pid(data, 10)["decision"]["status"] == "unknown"


def test_missing_kernel_and_other_account_do_not_block_readable_application_review():
    data = report(
        [
            observed(0, uid=0, missing="identity_or_query_unavailable"),
            observed(10, bundle=app()),
            observed(11, uid=0, missing="access_denied"),
            observed(12, missing="process_exited"),
        ]
    )
    assert not data["coverage"]["complete"]
    assert by_pid(data, 0)["decision"]["status"] == "protected"
    assert by_pid(data, 10)["decision"]["status"] == "review"


@pytest.mark.parametrize(
    "path,value",
    [
        (("schema_version",), 2),
        (("kind",), "memory_check.v0"),
        (("collection_ns",), True),
        (("memory_available", "value"), float("nan")),
        (("memory_available", "value"), 0.5),
        (("coverage", "observed"), 99),
        (("coverage", "complete"), False),
        (("groups", 0, "member_pids"), [10, 10]),
        (("groups", 0, "rss_total_bytes"), 0),
        (("groups", 0, "decision", "status"), "protected"),
        (("processes", 0, "decision", "reasons"), []),
        (("processes", 0, "rss", "missing_reason"), "unavailable"),
        (("processes", 0, "group_id"), "absent"),
    ],
)
def test_saved_contract_rejects_corruption(path, value):
    data = deepcopy(report([observed(10, bundle=app())]))
    target = data
    for key in path[:-1]:
        target = target[key]
    target[path[-1]] = value
    with pytest.raises(ContractError):
        validate_memory_check(data)


def test_saved_reader_rejects_duplicate_json_keys(tmp_path):
    path = tmp_path / "memory.json"
    path.write_text('{"kind":"memory_check.v1","kind":"memory_check.v1"}')
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        read_memory_check(path)


def test_snapshot_is_fresh_without_cross_command_cache():
    first = report([observed(10, bundle=app())])
    second = report([observed(12, bundle=app())])
    assert [r["pid"] for r in first["processes"]] == [10]
    assert [r["pid"] for r in second["processes"]] == [12]


def test_bundle_conflict_remains_unknown_for_descendants_in_any_enumeration_order():
    rows = [
        observed(10, bundle=app()),
        observed(11, parent=10, bundle=app("Other", "other")),
        observed(12, parent=11),
    ]
    for order in (rows, rows[::-1], [rows[1], rows[0], rows[2]]):
        data = report(order)
        assert by_pid(data, 11)["group_id"] is None
        assert by_pid(data, 12)["group_id"] is None
        assert by_pid(data, 12)["decision"]["status"] == "unknown"


def test_unavailable_protection_must_have_no_identity_and_a_reason():
    data = report([observed(10, bundle=app())], protected=[404])
    for field, value in (("reason", None), ("start_identity", 1)):
        corrupt = deepcopy(data)
        corrupt["protected_requests"][0][field] = value
        with pytest.raises(ContractError):
            validate_memory_check(corrupt)


def test_public_saved_samples_are_read_at_the_contract_boundary():
    from pathlib import Path

    root = Path(__file__).resolve().parents[1] / "fixtures/contracts"
    valid = read_memory_check(root / "memory-check.valid.json")
    assert {row["decision"]["status"] for row in valid["processes"]} == {
        "protected",
        "unknown",
        "review",
    }
    with pytest.raises(ContractError, match="count mismatch"):
        read_memory_check(root / "memory-check.invalid.json")


def test_mixed_null_reason_identity_is_rejected_as_contract_error():
    data = report([observed(10, bundle=app()), observed(12, bundle=app())])
    data["groups"][0]["decision"]["reasons"][0]["pid"] = None
    with pytest.raises(ContractError):
        validate_memory_check(data)


def test_usr_local_tool_is_unknown_instead_of_a_system_process():
    data = report([observed(10, executable="/usr/local/bin/tool")])
    assert by_pid(data, 10)["decision"]["status"] == "unknown"
    assert (
        by_pid(data, 10)["decision"]["reasons"][0]["code"] == "application_or_dependency_unverified"
    )


def test_parent_failure_saved_contract_keeps_readable_measurements_and_rejects_bad_edges():
    child = observed(11, parent=10, bundle=app("Child", "child"))
    child["parent_start_identity"] = None
    child["parent_identity_missing_reason"] = "parent_identity_unavailable"
    data = report([child, observed(20, bundle=app())])
    validate_memory_check(data)
    assert data["coverage"]["missing"] == 1
    assert by_pid(data, 11)["rss"]["value"] == 11 * 1024
    assert by_pid(data, 20)["decision"]["status"] == "review"
    for field, value in (("parent_start_identity", 1000), ("start_identity", None)):
        corrupt = deepcopy(data)
        by_pid(corrupt, 11)[field] = value
        with pytest.raises(ContractError):
            validate_memory_check(corrupt)
    corrupt = deepcopy(data)
    corrupt["coverage"]["missing"] = 0
    with pytest.raises(ContractError, match="count mismatch"):
        validate_memory_check(corrupt)

    corrupt = deepcopy(data)
    child = by_pid(corrupt, 11)
    child["decision"]["status"] = "review"
    next(group for group in corrupt["groups"] if group["group_id"] == child["group_id"])[
        "decision"
    ]["status"] = "review"
    with pytest.raises(ContractError, match="missing checks"):
        validate_memory_check(corrupt)
