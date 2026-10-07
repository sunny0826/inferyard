import shutil
from collections import Counter
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard import implementation_identity as identity
from inferyard.contracts.validation import ContractError


@pytest.fixture
def source_tree(tmp_path):
    root = Path(identity.__file__).parent
    target = tmp_path / "package"
    shutil.copytree(root, target, ignore=shutil.ignore_patterns("__pycache__"))
    return target


def test_one_read_per_file_and_role_boundaries(source_tree, monkeypatch):
    counts = Counter()
    original = Path.read_bytes

    def counted(path):
        counts[path] += 1
        return original(path)

    monkeypatch.setattr(Path, "read_bytes", counted)
    first = identity.IdentityContext(root=source_tree)
    assert set(counts.values()) == {1}
    for name in ("cli/metadata.py", "templates/report.html"):
        path = source_tree / name
        path.write_bytes(original(path) + b"\n")
    second = identity.IdentityContext(root=source_tree)
    assert first.source != second.source
    assert identity.execution_matches(first.value, second.value)
    assert first.value["presentation"] != second.value["presentation"]
    path = source_tree / "evidence/storage.py"
    path.write_bytes(original(path) + b"\n")
    third = identity.IdentityContext(root=source_tree)
    assert not identity.execution_matches(first.value, third.value)
    path = source_tree / "contracts/validation.py"
    path.write_bytes(original(path) + b"\n")
    fourth = identity.IdentityContext(root=source_tree)
    assert third.value["measurement"] != fourth.value["measurement"]
    assert third.value["scoring"] != fourth.value["scoring"]


def test_runtime_dependency_changes_only_relevant_role(source_tree, monkeypatch):
    first = identity.IdentityContext(root=source_tree)
    old = identity.version
    monkeypatch.setattr(
        identity, "version", lambda name: "changed" if name == "httpx" else old(name)
    )
    second = identity.IdentityContext(root=source_tree)
    assert first.value["measurement"] != second.value["measurement"]
    assert first.value["scoring"] == second.value["scoring"]
    assert first.value["presentation"] == second.value["presentation"]
    assert not any(
        d["name"] in {"ruff", "pytest"}
        for r in identity.ROLES
        for d in first.value[r]["dependencies"]
    )


def test_missing_dependency_is_not_equal_to_itself(source_tree, monkeypatch):
    old = identity.version

    def missing(name):
        if name == "jinja2":
            raise identity.PackageNotFoundError(name)
        return old(name)

    monkeypatch.setattr(identity, "version", missing)
    value = identity.IdentityContext(root=source_tree).value
    assert identity.role_identity(value, "presentation") is None
    assert identity.execution_matches(value, value)
    broken = deepcopy(value)
    broken["measurement"]["sha256"] = "a" * 64
    with pytest.raises(ContractError, match="digest mismatch"):
        identity.validate_identity(broken)


def test_comparison_v3_scope_dispatch_and_legacy_full_hash(tmp_path):
    from inferyard.analysis.comparison import compare_trials
    from inferyard.reporting.comparison_report import comparison_input
    from tests.helpers import fixture_run

    left = comparison_input(
        fixture_run(tmp_path, states=["completed"] * 3, comparison_mode="model")
    )[0]
    right = deepcopy(left)
    right["run"]["tool_source_sha256"] = "b" * 64
    scoped = compare_trials(left, right, definition="phase2.v3")
    assert scoped["eligibility"]["quality"] and scoped["eligibility"]["completion"]
    from inferyard.evidence.formats import UnsupportedFormat

    with pytest.raises(UnsupportedFormat):
        compare_trials(left, right, definition="phase2.v2")
    assert scoped["calibration_status"][0]["status"] == "not_supplied"
    assert all(r["difference"] is None for r in scoped["observed_differences"]["performance"])
    right["run"].pop("implementation_identity")
    assert not compare_trials(left, right, definition="phase2.v3")["eligibility"]["completion"]
    left["run"].pop("implementation_identity")
    assert not compare_trials(left, right, definition="phase2.v3")["eligibility"]["quality"]
    right["run"]["tool_source_sha256"] = left["run"]["tool_source_sha256"]
    assert compare_trials(left, right, definition="phase2.v3")["eligibility"]["quality"]
