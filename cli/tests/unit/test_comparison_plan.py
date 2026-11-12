"""Engine comparison plan freezing and its constrained frozen shape (ADR 057b)."""

import hashlib
import tomllib
from pathlib import Path

import pytest

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.contracts.bundle import content_hash
from inferyard.config.comparison_plan import (
    DEFAULT_BUNDLES,
    comparison_slots,
    freeze,
    validate_comparison_plan,
)
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.toml_writer import render
from inferyard.errors import ContractError

FIXTURES = Path(__file__).parents[1] / "fixtures"
ANSWER_POLICY = {
    "instruction_newlines": "crlf_to_lf",
    "strip_line_edges": True,
    "ignore_empty_lines": True,
    "unicode_normalization": "none",
    "reasoning": "separate_channel_excluded_content_unchanged",
}


def instruction_case(case_id: str = "instruction-01") -> JSONRecord:
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


def extraction_case(case_id: str = "extraction-01") -> JSONRecord:
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


def svg_case(case_id: str = "svg-pelican-01") -> JSONRecord:
    return {
        "case_id": case_id,
        "category": "svg",
        "prompt": "Generate an animated SVG of a pelican riding a bicycle",
        "reference_answer": "本题不设标准答案，仅展示模型生成结果，不做评分",
        "rules": {},
    }


def bundle_document(bundle_id: str, cases: list[JSONRecord]) -> JSONRecord:
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


def write_bundle(root: Path, name: str, bundle: JSONRecord) -> Path:
    from inferyard.contracts.validation import json_bytes

    path = root / f"{name}.json"
    path.write_bytes(json_bytes(bundle))
    return path


LOCAL_MODEL = {
    "content_sha256": "2" * 64,
    "assets_sha256": "1" * 64,
    "template_sha256": "3" * 64,
}


def _local_model(family: str) -> JSONRecord:
    kind = "directory" if family == "mlx-lm" else "gguf"
    return {
        "display_name": "Local fixture model",
        "repo": "fixture/local",
        "revision": "fixture-v1",
        "kind": kind,
        "local_path": "model" if kind == "directory" else "model.gguf",
        "packing": "4bit",
        "assets_path": "model-assets.json",
        "assets_sha256": LOCAL_MODEL["assets_sha256"],
        "content_sha256": LOCAL_MODEL["content_sha256"],
        "template_path": "template.jinja",
        "template_sha256": LOCAL_MODEL["template_sha256"],
    }


def _local_engine(root: Path, family: str) -> JSONRecord:
    environment = (
        {
            "PYTHONDONTWRITEBYTECODE": "1",
            "HF_HUB_OFFLINE": "1",
            "TRANSFORMERS_OFFLINE": "1",
        }
        if family == "mlx-lm"
        else {}
    )
    return {
        "adapter": "local_openai_v1",
        "family": family,
        "backend": "metal",
        "installation_path": "runtime.json",
        "installation_sha256": "4" * 64,
        "startup_args": ["-I", "-B", "/installed/server.py", "--model", "/model"],
        "working_directory": str(root),
        "environment": environment,
        "served_model": "fixture",
    }


def _local_config(
    root: Path,
    spec: JSONRecord,
    bundle_path: Path,
    bundle: JSONRecord,
) -> JSONRecord:
    family = spec["family"]
    base = tomllib.loads((FIXTURES / "config" / "valid.toml").read_text())
    base["definition"] = "local-engine-config.v1"
    base["model"] = _local_model(family)
    base["engine"] = _local_engine(root, family)
    base["conditions"].pop("threads", None)
    base["conditions"].pop("threads_batch", None)
    base["conditions"].pop("slots", None)
    base["conditions"]["cache_policy"] = "unknown"
    base["conditions"]["model_loaded"] = True
    base["safety"] = {
        "interval_seconds": 0.5,
        "max_temperature_celsius": 85,
        "require_temperature": True,
        "check_environment": True,
    }
    base["generation"] = {"temperature": 0, "max_tokens": spec.get("max_tokens", 32)}
    base["bundle"] = {"path": str(bundle_path), "version": bundle["version"]}
    base["endpoint"].pop("api_key_env", None)
    base.setdefault("execution", {}).update(warmup_count=0, timeout_seconds=30)
    base.setdefault("telemetry", {}).update(interval_ms=10, baseline_seconds=0.015)
    base["output"]["root"] = str(root / "results")
    runtime = spec.get("runtime")
    if runtime is not None:
        base["endpoint"].update(runtime.get("endpoint", {}))
    return base


def write_config(root: Path, name: str, spec: JSONRecord) -> Path:
    bundle = spec["bundle"]
    base = _local_config(root, spec, spec["bundle_path"], bundle)
    path = root / f"{name}.toml"
    path.write_text(render(base))
    return path

def comparison_inputs(
    tmp_path: Path,
    *,
    bundles: tuple[str, ...] = ("core", "pelican"),
    runtime: JSONRecord | None = None,
    left_family: str = "llama-cpp",
    right_family: str = "lmstudio",
) -> tuple[list[Path], list[Path], list[str]]:
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
            {
                "bundle_path": bundle_paths[key],
                "bundle": documents[key],
                "family": left_family,
                "runtime": runtime,
            },
        )
        for key in bundles
    ]
    right = [
        write_config(
            root,
            f"right-{key}",
            {
                "bundle_path": bundle_paths[key],
                "bundle": documents[key],
                "family": right_family,
                "runtime": runtime,
            },
        )
        for key in bundles
    ]
    specs = [str(path) for path in bundle_paths.values()]
    return left, right, specs


def test_two_bundles_freeze_six_blocks_and_fresh_each_slot(tmp_path: Path) -> None:
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


def test_default_product_bundles_are_used_when_no_bundle_is_declared(tmp_path: Path) -> None:
    from inferyard.provisioning.community_resources import resource_bytes

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
                {
                    "bundle_path": paths[name],
                    "bundle": documents[name],
                    "family": "llama-cpp",
                    "max_tokens": max_tokens,
                },
            )
        )
        right.append(
            write_config(
                root,
                f"right-{key}",
                {
                    "bundle_path": paths[name],
                    "bundle": documents[name],
                    "family": "lmstudio",
                    "max_tokens": max_tokens,
                },
            )
        )
    plan = freeze(left, right, destination=tmp_path / "plan")
    assert {slot.bundle_key for slot in comparison_slots(plan)} == {"core", "pelican"}
    assert len(plan["trials"]) == 12


def test_freeze_is_deterministic_and_previews_without_output(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    first = freeze(left, right, specs, experiment_id="engine-compare-fixture")
    second = freeze(left, right, specs, experiment_id="engine-compare-fixture")
    assert first["trials"] == second["trials"]
    assert first["experiment"] == second["experiment"]
    assert first["request_limit"] == second["request_limit"]
    assert first["experiment"]["experiment_id"] == "engine-compare-fixture"
    assert not (tmp_path / "plan").exists()


def test_explicit_single_bundle_freezes_six_slots(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path, bundles=("core",))
    plan = freeze(left, right, [specs[0]], destination=tmp_path / "plan")
    slots = comparison_slots(plan)
    assert len(slots) == 6
    assert {slot.bundle_key for slot in slots} == {"core"}


def test_default_scope_requires_both_declared_bundles(tmp_path: Path) -> None:
    left, right, _ = comparison_inputs(tmp_path, bundles=("core",))
    with pytest.raises(ContractError, match="do not cover"):
        freeze(left, right)


def test_bundle_content_must_match_across_sides(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    other = write_bundle(
        tmp_path / "inputs", "other", bundle_document("core", [instruction_case("x-01")])
    )
    right[0] = write_config(
        tmp_path / "inputs",
        "right-core-other",
        {
            "bundle_path": other,
            "bundle": bundle_document("core", [instruction_case("x-01")]),
            "family": "lmstudio",
        },
    )
    with pytest.raises(ContractError, match="content differs"):
        freeze(left, right, specs)


def test_side_profiles_must_not_be_mixed_across_bundles(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    mixed_left = [left[0], right[1]]
    mixed_right = [right[0], left[1]]
    out = tmp_path / "mixed-plan"
    with pytest.raises(ContractError, match="one engine profile per side"):
        freeze(mixed_left, mixed_right, specs, destination=out)
    assert not out.exists()
    with pytest.raises(ContractError, match="one engine profile per side"):
        freeze(mixed_left, mixed_right, specs)


def test_reread_plan_uses_the_current_support_matrix(tmp_path: Path) -> None:
    from inferyard.config.comparison_plan import (
        _bundle_documents,
        _experiment,
        _side_configs,
        _write_inputs,
    )
    from inferyard.config.planning import write_plan

    left, right, specs = comparison_inputs(
        tmp_path, left_family="lmstudio", right_family="mlx-lm"
    )
    experiment, files = _experiment(
        _side_configs(tuple(left)),
        _side_configs(tuple(right)),
        _bundle_documents(tuple(specs)),
        "unsupported-reread",
    )
    source_dir = tmp_path / "source"
    source_dir.mkdir()
    source = _write_inputs(source_dir, experiment, files)
    write_plan(source, tmp_path / "unsupported")
    frozen, loaded = read_frozen_plan(tmp_path / "unsupported" / "plan.json")
    with pytest.raises(ContractError, match="unsupported_engine_pair"):
        validate_comparison_plan(frozen, loaded)


def test_same_engine_on_both_sides_is_rejected(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    same = []
    for index, config in enumerate(right):
        path = tmp_path / "inputs" / f"same-{index}.toml"
        path.write_text(
            Path(config).read_text().replace('family = "lmstudio"', 'family = "llama-cpp"')
        )
        same.append(path)
    with pytest.raises(ContractError, match="engine_family_did_not_change"):
        freeze(left, same, specs)


def test_existing_output_directory_is_rejected_before_writing(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    out = tmp_path / "plan"
    out.mkdir()
    with pytest.raises(ContractError, match="must not exist"):
        freeze(left, right, specs, destination=out)
    assert list(out.iterdir()) == []


def test_frozen_plan_rejects_unplanned_shape(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    plan = freeze(left, right, specs)
    plan["experiment"]["workloads"] = plan["experiment"]["workloads"][:-1]
    plan["trials"] = plan["trials"][:-1]
    with pytest.raises(ContractError, match="six|complete"):
        comparison_slots(plan)


def test_default_bundle_names_are_the_declared_product_pair() -> None:
    assert DEFAULT_BUNDLES == ("zh-core", "zh-svg-pelican")


def test_compare_plan_cli_prints_the_frozen_plan(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
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


def test_unsupported_engine_pair_is_rejected_before_writing(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(
        tmp_path, left_family="lmstudio", right_family="mlx-lm"
    )
    with pytest.raises(ContractError, match="unsupported_engine_pair"):
        freeze(left, right, specs, destination=tmp_path / "plan")
    assert not (tmp_path / "plan").exists()


def test_external_profile_is_rejected_for_engine_comparison(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    local = tomllib.loads(Path(right[0]).read_text())
    config = tomllib.loads((FIXTURES / "config" / "valid.toml").read_text())
    config.update(bundle=local["bundle"], output=local["output"])
    path = tmp_path / "inputs" / "right-external.toml"
    path.write_text(render(config))
    right = [path, right[1]]
    with pytest.raises(ContractError, match="local engine profile"):
        freeze(left, right, specs)


def test_partial_temperature_override_is_rejected_before_writing(tmp_path: Path) -> None:
    left, right, specs = comparison_inputs(tmp_path)
    config = tomllib.loads(Path(left[0]).read_text())
    config["safety"]["temperature_stop_override_reason"] = "operator override"
    path = tmp_path / "inputs" / "left-override.toml"
    path.write_text(render(config))
    left = [path, left[1]]
    with pytest.raises(ContractError, match="override must cover"):
        freeze(left, right, specs)
    assert not (tmp_path / "plan").exists()


@pytest.mark.parametrize("family", ["mlx-lm"])
def test_safety_policy_admits_every_frozen_config(tmp_path: Path, family: str) -> None:
    from inferyard.runtime.local_profile import safety_policy

    left, right, specs = comparison_inputs(
        tmp_path, left_family="llama-cpp", right_family=family
    )
    plan = freeze(left, right, specs)
    from inferyard.config.plan_inputs import read_frozen_plan

    frozen, loaded = read_frozen_plan(tmp_path / "plan" / "plan.json") if (tmp_path / "plan").exists() else (plan, None)
    if loaded is None:
        from inferyard.config.comparison_plan import validate_comparison_plan

        # Freeze a second copy to obtain loaded configs from a real plan package.
        destination = tmp_path / "plan"
        freeze(left, right, specs, destination=destination)
        frozen, loaded = read_frozen_plan(destination / "plan.json")
    validate_comparison_plan(frozen, loaded)
    for workload in frozen["experiment"]["workloads"]:
        safety_policy(frozen, loaded[workload["workload_id"]].config.to_dict())


def _with_override(paths: list[Path], tmp_path: Path, prefix: str) -> list[Path]:
    changed = []
    for index, path in enumerate(paths):
        config = tomllib.loads(Path(path).read_text())
        config["safety"]["temperature_stop_override_reason"] = "operator approved"
        target = tmp_path / "inputs" / f"{prefix}-{index}.toml"
        target.write_text(render(config))
        changed.append(target)
    return changed


def test_temperature_override_covers_every_config_or_fails(tmp_path: Path) -> None:
    from inferyard.runtime.local_profile import safety_policy

    left, right, specs = comparison_inputs(tmp_path)
    destination = tmp_path / "plan"
    freeze(left, right, specs, destination=destination)
    from inferyard.config.plan_inputs import read_frozen_plan

    frozen, loaded = read_frozen_plan(destination / "plan.json")
    assert frozen["experiment"]["safety"]["max_temperature_celsius"] == 85
    for workload in frozen["experiment"]["workloads"]:
        safety_policy(frozen, loaded[workload["workload_id"]].config.to_dict())

    left = _with_override(left, tmp_path, "left-override")
    right = _with_override(right, tmp_path, "right-override")
    destination = tmp_path / "override-plan"
    freeze(left, right, specs, destination=destination)
    frozen, loaded = read_frozen_plan(destination / "plan.json")
    assert frozen["experiment"]["safety"]["max_temperature_celsius"] is None
    for workload in frozen["experiment"]["workloads"]:
        safety_policy(frozen, loaded[workload["workload_id"]].config.to_dict())
