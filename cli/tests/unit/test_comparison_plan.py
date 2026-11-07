"""Engine comparison plan freezing and its constrained frozen shape (ADR 057b)."""

import hashlib
import tomllib
from pathlib import Path

import pytest

from inferyard.config.bundle import content_hash
from inferyard.config.comparison_plan import (
    DEFAULT_BUNDLES,
    comparison_slots,
    freeze,
    validate_comparison_plan,
)
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.toml_writer import render
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError

FIXTURES = Path(__file__).parents[1] / "fixtures"
ANSWER_POLICY = {
    "instruction_newlines": "crlf_to_lf",
    "strip_line_edges": True,
    "ignore_empty_lines": True,
    "unicode_normalization": "none",
    "reasoning": "separate_channel_excluded_content_unchanged",
}


def instruction_case(case_id="instruction-01"):
    return {
        "case_id": case_id,
        "category": "instruction",
        "prompt": "只回答：北京",
        "reference_answer": "北京",
        "rules": {
            "literal_contains": ["北京"],
            "literal_forbidden": [],
            "nonempty_line_count": 1,
        },
    }


def extraction_case(case_id="extraction-01"):
    return {
        "case_id": case_id,
        "category": "extraction",
        "prompt": "姓名小明，年龄12岁。仅返回name与age的JSON。",
        "reference_answer": '{"name":"小明","age":12}',
        "rules": {
            "fields": {
                "name": {"type": "string", "value": "小明"},
                "age": {"type": "integer", "value": 12},
            },
            "allow_extra_fields": False,
        },
    }


def svg_case(case_id="svg-pelican-01"):
    return {
        "case_id": case_id,
        "category": "svg",
        "prompt": "Generate an animated SVG of a pelican riding a bicycle",
        "reference_answer": "本题不设标准答案，仅展示模型生成结果，不做评分",
        "rules": {},
    }


def bundle_document(bundle_id, cases):
    bundle = {
        "schema_version": 3,
        "bundle_id": bundle_id,
        "version": "1.0.0",
        "language": "zh-CN",
        "license_note": "Synthetic plan fixture; not a public corpus.",
        "review_records": [],
        "answer_policy": ANSWER_POLICY,
        "cases": cases,
        "task_protocol": "quality",
    }
    bundle["review_records"] = [
        {
            "reviewer": "synthetic-fixture",
            "reviewed_at": "2026-11-07T00:00:00Z",
            "content_sha256": content_hash(bundle),
            "conclusion": "approved",
        }
    ]
    return bundle


def write_bundle(root, name, bundle):
    from inferyard.evidence.storage import json_bytes

    path = root / f"{name}.json"
    path.write_bytes(json_bytes(bundle))
    return path


def write_config(root, name, bundle_path, bundle, adapter, *, max_tokens=32, runtime=None):
    base = tomllib.loads((FIXTURES / "config" / "valid.toml").read_text())
    if runtime is not None:
        for section in ("engine", "model", "conditions", "generation", "telemetry", "execution"):
            if section in runtime:
                from copy import deepcopy

                base[section] = deepcopy(runtime[section])
    base["bundle"] = {"path": str(bundle_path), "version": bundle["version"]}
    base["endpoint"].pop("api_key_env", None)
    base["engine"]["adapter"] = adapter
    base["generation"]["max_tokens"] = max_tokens
    base.setdefault("execution", {}).update(warmup_count=0, timeout_seconds=30)
    base.setdefault("telemetry", {}).update(interval_ms=10, baseline_seconds=0.015)
    base["output"]["root"] = str(root / "results")
    template = root / "fixture.jinja"
    template.write_bytes(b"fixture-template")
    base["model"]["template_path"] = str(template)
    base["model"]["template_sha256"] = hashlib.sha256(b"fixture-template").hexdigest()
    libraries = root / "libraries.json"
    libraries.write_text('{"fixture.so":"' + "a" * 64 + '"}')
    base["engine"]["runtime_library_manifest"] = str(libraries)
    path = root / f"{name}.toml"
    path.write_text(render(base))
    return path


def comparison_inputs(tmp_path, *, bundles=("core", "pelican"), runtime=None):
    root = tmp_path / "inputs"
    root.mkdir()
    core = bundle_document("core", [instruction_case(), extraction_case()])
    pelican = bundle_document("pelican", [svg_case()])
    documents = {"core": core, "pelican": pelican}
    bundle_paths = {
        key: write_bundle(root, key, documents[key]) for key in bundles
    }
    left = [
        write_config(
            root,
            f"left-{key}",
            bundle_paths[key],
            documents[key],
            "prism_llama_server_v1",
            runtime=runtime,
        )
        for key in bundles
    ]
    right = [
        write_config(
            root,
            f"right-{key}",
            bundle_paths[key],
            documents[key],
            "llama_cpp_b11146_v1",
            runtime=runtime,
        )
        for key in bundles
    ]
    specs = [str(path) for path in bundle_paths.values()]
    return left, right, specs


def test_two_bundles_freeze_six_blocks_and_fresh_each_slot(tmp_path):
    left, right, specs = comparison_inputs(tmp_path)
    plan = freeze(left, right, specs, destination=tmp_path / "plan")
    slots = comparison_slots(plan)
    assert [slot.block for slot in slots] == [1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6]
    assert [slot.side for slot in slots] == ["a", "a", "b", "b"] * 3
    assert [slot.bundle_key for slot in slots] == ["core", "pelican", "core", "pelican"] * 3
    assert slots[0].workload_id == "block-01-a-core"
    assert slots[-1].workload_id == "block-06-b-pelican"
    assert len(plan["trials"]) == 12
    assert len({trial["trial_id"] for trial in plan["trials"]}) == 12
    frozen, loaded = read_frozen_plan(tmp_path / "plan" / "plan.json")
    validate_comparison_plan(frozen, loaded)
    assert frozen["plan_sha256"] == plan["plan_sha256"]
    for workload in frozen["experiment"]["workloads"]:
        assert loaded[workload["workload_id"]].config.to_dict()["execution"][
            "require_fresh_process"
        ]
        assert "experiment" not in loaded[workload["workload_id"]].config.to_dict()


def test_default_product_bundles_are_used_when_no_bundle_is_declared(tmp_path):
    from inferyard.config.community_resources import resource_bytes

    root = tmp_path / "inputs"
    root.mkdir()
    paths, documents = {}, {}
    for name in DEFAULT_BUNDLES:
        raw = resource_bytes(f"bundles/{name}.json")
        path = root / f"{name}.json"
        path.write_bytes(raw)
        paths[name] = path
    import json

    documents = {name: json.loads(path.read_text()) for name, path in paths.items()}
    left, right = [], []
    for name in DEFAULT_BUNDLES:
        key = "core" if name == "zh-core" else "pelican"
        max_tokens = 512 if key == "core" else 4096
        left.append(
            write_config(
                root,
                f"left-{key}",
                paths[name],
                documents[name],
                "prism_llama_server_v1",
                max_tokens=max_tokens,
            )
        )
        right.append(
            write_config(
                root,
                f"right-{key}",
                paths[name],
                documents[name],
                "llama_cpp_b11146_v1",
                max_tokens=max_tokens,
            )
        )
    plan = freeze(left, right, destination=tmp_path / "plan")
    assert {slot.bundle_key for slot in comparison_slots(plan)} == {"core", "pelican"}
    assert len(plan["trials"]) == 12


def test_freeze_is_deterministic_and_previews_without_output(tmp_path):
    left, right, specs = comparison_inputs(tmp_path)
    first = freeze(left, right, specs, experiment_id="engine-compare-fixture")
    second = freeze(left, right, specs, experiment_id="engine-compare-fixture")
    assert first["trials"] == second["trials"]
    assert first["experiment"] == second["experiment"]
    assert first["request_limit"] == second["request_limit"]
    assert first["experiment"]["experiment_id"] == "engine-compare-fixture"
    assert not (tmp_path / "plan").exists()


def test_explicit_single_bundle_freezes_six_slots(tmp_path):
    left, right, specs = comparison_inputs(tmp_path, bundles=("core",))
    plan = freeze(left, right, [specs[0]], destination=tmp_path / "plan")
    slots = comparison_slots(plan)
    assert len(slots) == 6
    assert {slot.bundle_key for slot in slots} == {"core"}


def test_default_scope_requires_both_declared_bundles(tmp_path):
    left, right, _ = comparison_inputs(tmp_path, bundles=("core",))
    with pytest.raises(ContractError, match="do not cover"):
        freeze(left, right)


def test_bundle_content_must_match_across_sides(tmp_path):
    left, right, specs = comparison_inputs(tmp_path)
    other = write_bundle(
        tmp_path / "inputs", "other", bundle_document("core", [instruction_case("x-01")])
    )
    right[0] = write_config(
        tmp_path / "inputs",
        "right-core-other",
        other,
        bundle_document("core", [instruction_case("x-01")]),
        "llama_cpp_b11146_v1",
    )
    with pytest.raises(ContractError, match="content differs"):
        freeze(left, right, specs)


def test_same_engine_on_both_sides_is_rejected(tmp_path):
    left, right, specs = comparison_inputs(tmp_path)
    same = []
    for index, config in enumerate(right):
        path = tmp_path / "inputs" / f"same-{index}.toml"
        path.write_text(
            Path(config).read_text().replace("llama_cpp_b11146_v1", "prism_llama_server_v1")
        )
        same.append(path)
    with pytest.raises(ContractError, match="sides must differ"):
        freeze(left, same, specs)


def test_existing_output_directory_is_rejected_before_writing(tmp_path):
    left, right, specs = comparison_inputs(tmp_path)
    out = tmp_path / "plan"
    out.mkdir()
    with pytest.raises(ContractError, match="must not exist"):
        freeze(left, right, specs, destination=out)
    assert list(out.iterdir()) == []


def test_frozen_plan_rejects_unplanned_shape(tmp_path):
    left, right, specs = comparison_inputs(tmp_path)
    plan = freeze(left, right, specs)
    plan["experiment"]["workloads"] = plan["experiment"]["workloads"][:-1]
    plan["trials"] = plan["trials"][:-1]
    with pytest.raises(ContractError, match="six|complete"):
        comparison_slots(plan)


def test_default_bundle_names_are_the_declared_product_pair():
    assert DEFAULT_BUNDLES == ("zh-core", "zh-svg-pelican")


def test_compare_plan_cli_prints_the_frozen_plan(tmp_path, capsys):
    import json

    from inferyard.cli import main

    left, right, specs = comparison_inputs(tmp_path)
    arguments = ["compare-plan"]
    for path in left:
        arguments += ["--left-config", str(path)]
    for path in right:
        arguments += ["--right-config", str(path)]
    for spec in specs:
        arguments += ["--bundle", spec]
    arguments += ["--out", str(tmp_path / "plan")]
    assert main(arguments) == 0
    saved = json.loads(capsys.readouterr().out)
    assert saved["command"] == "compare-plan"
    assert len(saved["details"]["trials"]) == 12
    assert (tmp_path / "plan" / "plan.json").exists()
