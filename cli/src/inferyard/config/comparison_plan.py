"""Freeze one constrained engine comparison plan (engine-comparison-plan.v1).

The plan is the existing plan JSON under a fixed shape: six blocks, alternating
sides, one workload per side and bundle. It does not add an outer document or a
new plan/run field, and every referenced config freezes
``execution.require_fresh_process = true``.
"""

from __future__ import annotations

import hashlib
import re
from dataclasses import dataclass
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import TYPE_CHECKING

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison_types import JSONRecord, JSONValue
from inferyard.analysis.engine_comparison import config_pair_blockers
from inferyard.analysis.scoring import SCORER_VERSION
from inferyard.provisioning.community_resources import BUNDLES, resource_bytes
from inferyard.config.loader import load_config, read_document
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.comparison_plan_safety import (
    comparison_budget,
    comparison_overhead,
    require_bundle_content,
    comparison_safety,
)
from inferyard.config.planning import write_plan
from inferyard.contracts.engine_profiles import is_local
from inferyard.contracts.validation import strict_json_loads, validate_document, json_bytes
from inferyard.errors import ContractError
from inferyard.evidence.formats import require_input

if TYPE_CHECKING:
    from inferyard.config.loader import LoadedConfig

DEFINITION = "engine-comparison-plan.v1"
BLOCKS = 6
DEFAULT_BUNDLES = ("zh-core", "zh-svg-pelican")
BUNDLE_KEYS = {"zh-core": "core", "zh-core-v2": "core", "zh-svg-pelican": "pelican"}
_SLOT = re.compile(r"^block-(\d{2})-([ab])-(.+)$")
_IDENTIFIER = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")


@dataclass(frozen=True, slots=True)
class Slot:
    block: int
    side: str
    bundle_key: str
    workload_id: str
    trial_id: str


def slot_side(block: int) -> str:
    return "a" if block % 2 else "b"


def slot_id(block: int, side: str, bundle_key: str) -> str:
    return f"block-{block:02d}-{side}-{bundle_key}"


def bundle_key(bundle: JSONRecord) -> str:
    identifier = bundle.get("bundle_id")
    if identifier in BUNDLE_KEYS:
        return BUNDLE_KEYS[identifier]
    if type(identifier) is not str or not _IDENTIFIER.fullmatch(identifier):
        raise ContractError(
            "compare_plan.bundle", "bundle id is not a valid workload identifier"
        )
    return identifier


def _parsed_workloads(workloads: list[JSONRecord]) -> list[tuple[int, str, str, JSONRecord]]:
    parsed = []
    for workload in workloads:
        match = _SLOT.fullmatch(workload.get("workload_id", ""))
        if match is None:
            raise ContractError(
                "plan.workloads.workload_id",
                "workload id must be block-NN-[ab]-<bundle>",
            )
        parsed.append((int(match[1]), match[2], match[3], workload))
    return parsed


def _bundle_keys(parsed: list[tuple[int, str, str, JSONRecord]]) -> list[str]:
    keys = [item[2] for item in parsed if item[0] == 1]
    if len(keys) not in (1, 2) or len(set(keys)) != len(keys):
        raise ContractError("plan.workloads", "one or two bundle workloads per block required")
    return keys


def _frozen_slot_error(
    position: int,
    item: tuple[int, str, str, JSONRecord],
    trial: JSONRecord,
    keys: list[str],
) -> str | None:
    block, side, key, workload = item
    expected_block, expected_key = divmod(position, len(keys))
    expected_block += 1
    if (
        block != expected_block
        or key != keys[expected_key]
        or side != slot_side(block)
        or workload["workload_id"] != slot_id(block, side, key)
    ):
        return "blocks, sides or bundle order are not frozen plan order"
    if workload["protocol"]["kind"] != "fixed" or workload["repeats"] != 1:
        return "comparison slots require one fixed trial"
    if (
        trial["workload_id"] != workload["workload_id"]
        or trial["repeat_index"] != 0
        or trial.get("requires_service_handoff") is not True
    ):
        return "trial does not match its frozen slot"
    return None


def _require_frozen_order(
    parsed: list[tuple[int, str, str, JSONRecord]],
    trials: list[JSONRecord],
    keys: list[str],
) -> None:
    if len(parsed) != BLOCKS * len(keys) or len(trials) != len(parsed):
        raise ContractError("plan.workloads", f"exactly {BLOCKS} complete blocks required")
    for position, (item, trial) in enumerate(zip(parsed, trials, strict=True)):
        error = _frozen_slot_error(position, item, trial, keys)
        if error is not None:
            raise ContractError("plan.comparison", error)


def comparison_slots(plan: JSONRecord) -> tuple[Slot, ...]:
    """Return frozen slots in execution order; reject any other plan shape."""
    experiment = plan.get("experiment") if type(plan) is dict else None
    if type(experiment) is not dict or experiment.get("comparison") != {
        "mode": "engine",
        "factor": None,
    }:
        raise ContractError("plan.comparison", f"{DEFINITION} requires comparison.mode engine")
    workloads = experiment.get("workloads")
    trials = plan.get("trials")
    if type(workloads) is not list or not workloads or type(trials) is not list:
        raise ContractError("plan.workloads", f"{DEFINITION} requires workloads and trials")
    parsed = _parsed_workloads(workloads)
    keys = _bundle_keys(parsed)
    _require_frozen_order(parsed, trials, keys)
    return tuple(
        Slot(block, side, key, workload["workload_id"], trials[index]["trial_id"])
        for index, (block, side, key, workload) in enumerate(parsed)
    )


def _profile_key(config: JSONRecord) -> tuple[str, str]:
    engine = config["engine"]
    return ("local" if is_local(config) else "external", engine.get("family") or engine["adapter"])


def _validate_config_profiles(
    left: dict[str, JSONRecord], right: dict[str, JSONRecord], *, prefix: str
) -> None:
    sides = {"left": left, "right": right}
    profiles = {}
    for label, side in sides.items():
        if not side:
            raise ContractError(f"{prefix}.config", "at least one config per side required")
        if not all(is_local(config) for config in side.values()):
            raise ContractError(
                f"{prefix}.profile", "engine comparison requires local engine profiles"
            )
        values = {_profile_key(config) for config in side.values()}
        if len(values) != 1:
            raise ContractError(f"{prefix}.config", "one engine profile per side required")
        profile = next(iter(values))
        if profile[1] is None:
            raise ContractError(f"{prefix}.engines", "local profile requires a family")
        profiles[label] = profile
    for key in sorted(left):
        blockers = config_pair_blockers(left[key], right[key])
        if blockers:
            raise ContractError(f"{prefix}.engines", "; ".join(blockers))


def validate_comparison_plan(
    plan: JSONRecord, loaded: dict[str, LoadedConfig]
) -> tuple[Slot, ...]:
    """Apply the per-side and fresh-process rules that need loaded configs."""
    slots = comparison_slots(plan)
    workloads = {w["workload_id"]: w for w in plan["experiment"]["workloads"]}
    configs, bundles = {}, {}
    side_configs = {"a": {}, "b": {}}
    for slot in slots:
        config = loaded[slot.workload_id].config.to_dict()
        if config["execution"].get("require_fresh_process") is not True:
            raise ContractError(
                "plan.comparison.fresh_process",
                "every comparison slot requires execution.require_fresh_process = true",
            )
        key = (slot.side, slot.bundle_key)
        reference = workloads[slot.workload_id]
        if key in configs and configs[key] != reference["config"]["sha256"]:
            raise ContractError(
                "plan.comparison.config", "one frozen config per side and bundle required"
            )
        if slot.bundle_key in bundles and bundles[slot.bundle_key] != reference["bundle"]["sha256"]:
            raise ContractError(
                "plan.comparison.bundle", "one frozen bundle per comparison slot required"
            )
        configs[key] = reference["config"]["sha256"]
        bundles[slot.bundle_key] = reference["bundle"]["sha256"]
        side_configs[slot.side].setdefault(slot.bundle_key, config)
    _validate_config_profiles(side_configs["a"], side_configs["b"], prefix="plan.comparison")
    return slots


def is_comparison_plan(plan: JSONValue) -> bool:
    try:
        slots = comparison_slots(plan)
    except (ContractError, KeyError, TypeError, ValueError):
        return False
    return bool(slots)


def _bundle_documents(specs: tuple[str, ...]) -> dict[str, tuple[bytes, JSONRecord]]:
    if not specs:
        specs = DEFAULT_BUNDLES
    documents = {}
    for spec in specs:
        if spec in BUNDLES:
            raw = resource_bytes(f"bundles/{spec}.json")
        else:
            raw = read_document(Path(spec).expanduser().resolve())
        try:
            bundle = strict_json_loads(raw.decode("utf-8"))
        except UnicodeError as exc:
            raise ContractError("compare_plan.bundle", "invalid UTF-8 bundle") from exc
        require_input(bundle, "bundle")
        validate_document("bundle", bundle)
        key = bundle_key(bundle)
        if key in documents:
            raise ContractError("compare_plan.bundle", "duplicate bundle supplied")
        documents[key] = (raw, bundle)
    if len(documents) not in (1, 2):
        raise ContractError("compare_plan.bundle", "one or two bundles required")
    return documents


def _side_configs(
    paths: tuple[Path, ...],
) -> dict[str, tuple[LoadedConfig, bytes, JSONRecord]]:
    if not paths:
        raise ContractError("compare_plan.config", "at least one config per side required")
    result = {}
    for path in paths:
        loaded = load_config(path)
        config = loaded.config.to_dict()
        raw = read_document(Path(config["bundle"]["path"]))
        bundle = loaded.bundle.to_dict()
        key = bundle_key(bundle)
        if key in result:
            raise ContractError("compare_plan.config", "duplicate config for one bundle")
        result[key] = (loaded, raw, bundle)
    return result


def _require_coverage(
    left: dict[str, JSONRecord], right: dict[str, JSONRecord], expected: dict[str, JSONRecord]
) -> None:
    for label, side in (("left", left), ("right", right)):
        missing = sorted(set(expected) - set(side))
        extra = sorted(set(side) - set(expected))
        if missing or extra:
            raise ContractError(
                "compare_plan.bundle",
                f"{label} configs do not cover the declared bundles"
                + (f"; missing {missing}" if missing else "")
                + (f"; extra {extra}" if extra else ""),
            )


def _experiment_id(left: str | None, right: str | None, override: str | None) -> str:
    if override is not None:
        if not _IDENTIFIER.fullmatch(override):
            raise ContractError("compare_plan.experiment_id", "invalid experiment id")
        return override
    parts = [
        re.sub(r"[^A-Za-z0-9_.:-]+", "-", side).strip("-") or "engine"
        for side in (left, right)
    ]
    return ("engine-compare-" + "-vs-".join(parts))[:128]


def _side_workloads(
    side: str,
    configs: dict[str, tuple[LoadedConfig, bytes, JSONRecord]],
    files: dict[str, bytes],
) -> list[JSONRecord]:
    workloads = []
    for key, (loaded, raw, bundle) in configs.items():
        config = loaded.config.to_dict()
        config["bundle"]["path"] = f"bundle-{key}.json"
        config["execution"]["require_fresh_process"] = True
        config_path = f"{side}-{key}-config.json"
        payload = json_bytes(config)
        files[config_path] = payload
        files[f"bundle-{key}.json"] = raw
        cases = [case["case_id"] for case in bundle["cases"]]
        for block in range(1, BLOCKS + 1):
            if slot_side(block) != side:
                continue
            workloads.append(
                {
                    "block": block,
                    "side": side,
                    "key": key,
                    "config": config,
                    "config_path": config_path,
                    "config_sha256": hashlib.sha256(payload).hexdigest(),
                    "bundle_sha256": hashlib.sha256(raw).hexdigest(),
                    "cases": cases,
                    "bundle": bundle,
                }
            )
    return workloads


def _definition(item: JSONRecord) -> tuple[JSONRecord, int, float]:
    config = item["config"]
    limit = len(item["cases"])
    return {
        "workload_id": slot_id(item["block"], item["side"], item["key"]),
        "purpose": "performance"
        if item["bundle"].get("task_protocol") == "performance"
        else "quality",
        "config": {"path": item["config_path"], "sha256": item["config_sha256"]},
        "bundle": {
            "path": f"bundle-{item['key']}.json",
            "sha256": item["bundle_sha256"],
        },
        "protocol": {"kind": "fixed", "case_ids": item["cases"]},
        "repeats": 1,
        "timeout_seconds": config["execution"]["timeout_seconds"],
        "overhead_budget_seconds": comparison_overhead(config, limit),
        "input_target_tokens": None,
        "output_budget_tokens": config["generation"]["max_tokens"],
    }, limit, limit * config["execution"]["timeout_seconds"] + comparison_overhead(config, limit)


def _experiment(
    left: dict[str, tuple[LoadedConfig, bytes, JSONRecord]],
    right: dict[str, tuple[LoadedConfig, bytes, JSONRecord]],
    bundles: dict[str, tuple[bytes, JSONRecord]],
    experiment_id: str,
) -> tuple[JSONRecord, dict[str, bytes]]:
    files, workloads = {}, []
    workloads.extend(_side_workloads("a", left, files))
    workloads.extend(_side_workloads("b", right, files))
    workloads.sort(key=lambda item: (item["block"], list(bundles).index(item["key"])))
    definitions, limits = [], []
    for item in workloads:
        definition, limit, total = _definition(item)
        definitions.append(definition)
        limits.append((limit, total))
    configs = [loaded.config.to_dict() for side in (left, right) for loaded, _, _ in side.values()]
    experiment = {
        "schema_version": SCHEMA_VERSION,
        "experiment_id": experiment_id,
        "name": f"Engine comparison: {experiment_id}",
        "definition_versions": {
            "measurement": "phase2.v1",
            "scoring": SCORER_VERSION,
            "comparison": "phase2.v1",
        },
        "execution": {
            "concurrency": 1,
            "automatic_retries": 0,
            "order": "fixed",
            "seed": None,
            "service_transition": "operator_verified",
        },
        "comparison": {"mode": "engine", "factor": None},
        "budget": comparison_budget(configs, limits),
        "workloads": definitions,
    }
    safety = comparison_safety(configs)
    if safety is not None:
        experiment["safety"] = safety
    return experiment, files


def _side_profiles(
    left: dict[str, tuple[LoadedConfig, bytes, JSONRecord]],
    right: dict[str, tuple[LoadedConfig, bytes, JSONRecord]],
) -> tuple[str, str]:
    left_configs = {key: loaded.config.to_dict() for key, (loaded, _, _) in left.items()}
    right_configs = {key: loaded.config.to_dict() for key, (loaded, _, _) in right.items()}
    _validate_config_profiles(left_configs, right_configs, prefix="compare_plan")
    left_name = _profile_key(next(iter(left_configs.values())))[1]
    right_name = _profile_key(next(iter(right_configs.values())))[1]
    return left_name, right_name


def _comparison_inputs(
    left_configs: tuple[Path, ...],
    right_configs: tuple[Path, ...],
    bundle_specs: tuple[str, ...],
    experiment_id: str | None,
) -> tuple[JSONRecord, dict[str, bytes]]:
    expected = _bundle_documents(tuple(bundle_specs))
    left = _side_configs(tuple(left_configs))
    right = _side_configs(tuple(right_configs))
    _require_coverage(left, right, expected)
    require_bundle_content(left, right, expected)
    left_name, right_name = _side_profiles(left, right)
    identity = _experiment_id(left_name, right_name, experiment_id)
    return _experiment(left, right, expected, identity)


def _write_inputs(root: Path, experiment: JSONRecord, files: dict[str, bytes]) -> Path:
    root.mkdir(parents=True, exist_ok=True)
    for name, raw in files.items():
        (root / name).write_bytes(raw)
    source = root / "experiment.json"
    source.write_bytes(json_bytes(experiment))
    return source


def freeze(
    left_configs: tuple[Path, ...],
    right_configs: tuple[Path, ...],
    bundle_specs: tuple[str, ...] = (),
    *,
    experiment_id: str | None = None,
    destination: Path | None = None,
) -> JSONRecord:
    """Freeze one comparison plan; ``destination=None`` previews without writing."""
    experiment, files = _comparison_inputs(
        left_configs, right_configs, bundle_specs, experiment_id
    )
    if destination is not None and Path(destination).exists():
        raise ContractError("compare_plan.out", "output directory must not exist")
    with TemporaryDirectory(prefix="inferyard-compare-plan-") as temporary:
        source = _write_inputs(Path(temporary) / "source", experiment, files)
        write_plan(source, Path(temporary) / "plan")
        validated, loaded = read_frozen_plan(Path(temporary) / "plan" / "plan.json")
        slots = validate_comparison_plan(validated, loaded)
        if not slots:
            raise ContractError("compare_plan.workloads", "comparison plan has no slots")
        if destination is None:
            return validated
        return write_plan(source, Path(destination))
