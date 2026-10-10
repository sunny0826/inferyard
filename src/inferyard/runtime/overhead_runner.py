"""Execute a bounded ABBA preflight through the normal trial lifecycle."""

import asyncio
import hashlib
import time
from dataclasses import replace

from inferyard import SCHEMA_VERSION
from inferyard.evidence.journal import trial_for
from inferyard.evidence.storage import atomic_bytes, json_bytes, sha256_file
from inferyard.platforms.resources import ResourceSampler, resource_collector_id
from inferyard.platforms.telemetry import Sampler
from inferyard.provenance import tool_source_hash
from inferyard.registry import adapter_factory
from inferyard.runtime.boundary_observer import BoundaryOnlySampler
from inferyard.runtime.collector_overhead import ORDER, assess_overhead, freeze_protocol
from inferyard.runtime.environment_observer import EnvironmentOnlySampler
from inferyard.runtime.trial_runner import TrialDependencies, run_trial


class ResourceOffSampler(Sampler):
    """Disable periodic resource/environment sampling and boundary collection."""

    def __init__(self, store, config):
        super().__init__(store, config)
        store.snapshot(
            "collector.json",
            {
                "schema_version": SCHEMA_VERSION,
                "collector": "resource_off",
                "scope": "periodic_and_boundary_collection_disabled",
            },
        )

    def collect(self, pid, ticks):
        return []

    async def run(self):
        while not self.stopped:
            await asyncio.sleep(self.config["telemetry"]["interval_ms"] / 1000)


def request_projection(row, *, first_events=False, engine_rates=False, block_gaps=False):
    left, right = row.get("t_send_ns"), row.get("t_terminal_ns")
    payload = {
        key: row.get(key)
        for key in ("content", "reasoning", "completion_tokens", "token_source", "token_scope")
    }
    known_tokens = (
        type(row.get("completion_tokens")) is int
        and row["completion_tokens"] >= 0
        and bool(row.get("token_source"))
        and bool(row.get("token_scope"))
    )
    result = {
        "case_id": row["case_id"],
        "execution_state": row["execution_state"],
        "duration_ns": right - left if left is not None and right is not None else None,
        "output_identity": hashlib.sha256(json_bytes(payload)).hexdigest()
        if known_tokens
        else None,
    }
    if first_events:
        from inferyard.analysis.first_event_overhead import projection

        result["first_event_ns"] = projection(row)
    if engine_rates:
        from inferyard.analysis.engine_overhead import projection

        result["engine_rates"] = projection(row)
    if block_gaps:
        from inferyard.analysis.block_overhead import projection

        result["block_gaps"] = projection(row)
    return result


async def run_overhead(
    plan,
    trial_id,
    loaded,
    output_root,
    *,
    tolerance_ratio,
    max_wall_seconds,
    dependencies=None,
    common_observer=False,
    boundary_observer=False,
    first_event_tolerance_ratio=None,
    engine_rate_tolerance_ratio=None,
    block_gap_tolerance_ms=None,
):
    """New output directory required; interruption never retries a slot implicitly.

    This is a diagnostic preflight, not a quality run. The manifest and frozen
    protocol retain the complete input identities before any workload requests.
    """
    from inferyard.analysis.scoring import ScoringContext
    from inferyard.implementation_identity import IdentityContext

    identity_context, scoring_context = (
        IdentityContext(source_hash=tool_source_hash),
        ScoringContext(),
    )
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    deps = dependencies or TrialDependencies(adapter=adapter_factory(config["engine"]["adapter"]))
    trial = trial_for(plan, trial_id)
    workload = next(
        w for w in plan["experiment"]["workloads"] if w["workload_id"] == trial["workload_id"]
    )
    if workload["protocol"]["kind"] != "fixed":
        from inferyard.evidence.storage import EvidenceError

        raise EvidenceError("overhead_requires_fixed_workload")
    protocol = freeze_protocol(
        case_ids=trial["case_order"],
        interval_ms=config["telemetry"]["interval_ms"],
        tolerance_ratio=tolerance_ratio,
        max_wall_seconds=max_wall_seconds,
        config_sha256=hashlib.sha256(json_bytes(config)).hexdigest(),
        bundle_sha256=hashlib.sha256(json_bytes(bundle)).hexdigest(),
        tool_source_sha256=identity_context.source,
        common_observer=common_observer,
        boundary_observer=boundary_observer,
        first_event_tolerance_ratio=first_event_tolerance_ratio,
        engine_rate_tolerance_ratio=engine_rate_tolerance_ratio,
        block_gap_tolerance_ms=block_gap_tolerance_ms,
        resource_collector=resource_collector_id(),
    )
    output_root.mkdir(parents=True, exist_ok=False)
    atomic_bytes(output_root / "protocol.json", json_bytes(protocol))
    trials = []
    atomic_bytes(output_root / "trials.json", json_bytes(trials))
    code = 0
    began = time.monotonic()
    try:
        with deps.lock() as lock:
            for index, mode in enumerate(ORDER):
                remaining = max_wall_seconds - (time.monotonic() - began)
                if remaining <= 0:
                    break
                off_sampler = EnvironmentOnlySampler if common_observer else ResourceOffSampler
                if boundary_observer:
                    off_sampler = BoundaryOnlySampler
                local_deps = replace(deps, sampler=ResourceSampler if mode == "on" else off_sampler)
                code, data, path = await run_trial(
                    plan,
                    trial_id,
                    loaded,
                    output_root / f"{index + 1:02}-{mode}",
                    diagnostic=True,
                    dependencies=local_deps,
                    host_lock=lock,
                    wall_budget_seconds=remaining,
                    source_identity=identity_context.source,
                    implementation_identity=identity_context.value,
                    scoring_context=scoring_context,
                )
                row = {
                    "mode": mode,
                    "protocol_sha256": protocol["protocol_sha256"],
                    "run_id": data["run"]["run_id"],
                    "completed": code == 0 and data["summary"]["stop_reason"] == "plan_finished",
                    "path": str(path.relative_to(output_root)),
                    "manifest_sha256": sha256_file(path / "manifest.json"),
                    "requests": [
                        request_projection(
                            r,
                            first_events="first_event_contract" in protocol,
                            engine_rates="engine_rate_contract" in protocol,
                            block_gaps="block_gap_contract" in protocol,
                        )
                        for r in data["requests"]
                    ],
                }
                trials.append(row)
                atomic_bytes(output_root / "trials.json", json_bytes(trials), overwrite=True)
                if code != 0:
                    break
    finally:
        result = assess_overhead(protocol, trials)
        result["execution_exit_code"] = code
        result["elapsed_seconds"] = time.monotonic() - began
        result["performance_comparison_eligible"] = False
        atomic_bytes(output_root / "result.json", json_bytes(result))
    return result


def read_overhead(root, *, target=None, target_data=None):
    """Recompute from sealed trials; do not trust cached request projections/results."""
    from inferyard.analysis.overhead_environment import assess_environments, environment_record
    from inferyard.evidence.ledger import read_trial
    from inferyard.evidence.storage import EvidenceError, local_file, read_json
    from inferyard.runtime.collector_overhead import validate_protocol
    from inferyard.runtime.overhead_binding import bind_target, workload_identity

    protocol = read_json(local_file(root, "protocol.json"))
    validate_protocol(protocol)
    index = read_json(local_file(root, "trials.json"))
    trials, seen = [], set()
    arm_identity = None
    environments = []
    for entry in index:
        path = local_file(root, entry["path"])
        data = read_trial(path)
        if target is not None:
            environments.append(
                environment_record(
                    path, data, require_boundary=protocol["kind"] == "collector_overhead_abba.v3"
                )
            )
        identity = workload_identity(data)
        if arm_identity is not None and identity != arm_identity:
            raise EvidenceError("overhead_arm_workload_mismatch")
        arm_identity = identity
        if data["config"]["telemetry"]["interval_ms"] != protocol["interval_ms"]:
            raise EvidenceError("overhead_interval_mismatch")
        run_id = data["run"]["run_id"]
        if run_id in seen or run_id != entry["run_id"]:
            raise EvidenceError("overhead_duplicate_or_mismatched_run")
        seen.add(run_id)
        if sha256_file(path / "manifest.json") != entry["manifest_sha256"]:
            raise EvidenceError("overhead_manifest_mismatch")
        if data["run"].get("tool_source_sha256") != protocol["tool_source_sha256"]:
            raise EvidenceError("overhead_tool_mismatch")
        for name, filename in (("config", "config.frozen.json"), ("bundle", "bundle.json")):
            if sha256_file(path / filename) != protocol[name + "_sha256"]:
                raise EvidenceError("overhead_frozen_input_mismatch")
        sealed = read_json(path / "manifest.json")["files"]
        if "collector.json" not in sealed:
            raise EvidenceError("overhead_collector_unsealed")
        collector = read_json(local_file(path, "collector.json"))
        off_collector = (
            "environment-only.v1"
            if protocol["kind"] == "collector_overhead_abba.v2"
            else "resource_off"
        )
        if protocol["kind"] == "collector_overhead_abba.v3":
            off_collector = "boundary-only.v1"
            if "boundary-observer.json" not in sealed or (
                read_json(local_file(path, "boundary-observer.json"))
                != protocol["boundary_observer"]
            ):
                raise EvidenceError("overhead_boundary_observer_mismatch")
        if protocol["kind"] == "collector_overhead_abba.v2":
            if "observer.json" not in sealed or (
                read_json(local_file(path, "observer.json")) != protocol["observer"]
            ):
                raise EvidenceError("overhead_observer_mismatch")
        mode = "off" if collector["collector"] == off_collector else "on"
        if (
            collector["collector"]
            not in (off_collector, protocol.get("resource_collector", "linux-resource.v2"))
            or mode != entry["mode"]
        ):
            raise EvidenceError("overhead_collector_mismatch")
        if mode == "off" and data["samples"]:
            raise EvidenceError("overhead_off_arm_has_samples")
        if mode == "on" and not data["samples"]:
            raise EvidenceError("overhead_on_arm_missing_samples")
        trials.append(
            {
                **entry,
                "completed": data["summary"]["stop_reason"] == "plan_finished",
                "requests": [
                    request_projection(
                        r,
                        first_events="first_event_contract" in protocol,
                        engine_rates="engine_rate_contract" in protocol,
                        block_gaps="block_gap_contract" in protocol,
                    )
                    for r in data["requests"]
                ],
            }
        )
    result = assess_overhead(protocol, trials)
    result["performance_comparison_eligible"] = False
    if target is not None:
        data = target_data if target_data is not None else read_trial(target)
        result["target_binding"] = bind_target(
            target,
            data,
            protocol,
            arm_identity,
            trials[0]["requests"] if trials else [],
            result,
            read_json(local_file(target, "collector.json")),
        )
        result["environment_binding"] = assess_environments(
            environments,
            environment_record(
                target, data, require_boundary=protocol["kind"] == "collector_overhead_abba.v3"
            ),
        )
    return result
