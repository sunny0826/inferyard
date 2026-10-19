"""Strict offline reduction of a single repetition or explicitly partial resume."""

from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.aggregate import rate
from inferyard.analysis.environment import measurement_context
from inferyard.analysis.idle_rss import idle_rss_observations
from inferyard.analysis.input_lengths import (
    bind_token_counts,
    check_input_target,
    input_length_summary,
)
from inferyard.analysis.observations import build_observations
from inferyard.analysis.output_budget import output_budget_observations
from inferyard.analysis.performance import summarize_performance
from inferyard.analysis.position import position_summary
from inferyard.analysis.quality import summarize_quality
from inferyard.analysis.resource_observations import build_resource_observations
from inferyard.analysis.stability_observations import latency_drift_observations
from inferyard.config.plan_inputs import validate_workload_inputs
from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document
from inferyard.evidence.capacity_stop import validate_capacity_stop
from inferyard.evidence.duration_ledger import duration_summary
from inferyard.evidence.engine_capabilities import observation_sealed
from inferyard.evidence.event_ledger import reduce_events
from inferyard.evidence.formats import UnsupportedFormat, require_core
from inferyard.evidence.journal import trial_for
from inferyard.evidence.storage import (
    EvidenceError,
    json_bytes,
    verify_manifest,
)
from inferyard.evidence.token_budgets import read_budgets
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.evidence.trial_summary import trial_summary

CORE = {
    "run.json",
    "plan.json",
    "selection.json",
    "config.frozen.json",
    "bundle.json",
    "events.jsonl",
    "memory.jsonl",
}


CORE_LAYOUT2 = {
    "inputs.json",
    "observations.jsonl",
    "request-bodies.jsonl",
    "events.jsonl",
    "memory.jsonl",
    "environment.jsonl",
    "schedule.jsonl",
}


def core_files(reads):
    return CORE_LAYOUT2 if reads.layout_version == 2 else CORE


def preload_inputs(reads):
    for kind in ("run", "plan", "selection", "config", "bundle"):
        try:
            reads.input(kind)
        except EvidenceError as exc:
            if exc not in reads.errors.values():
                raise
            # Defer physical parse failures until the original seal has been checked.


def _inputs(root, reads, limits):
    documents = {}
    for kind in ("run", "plan", "selection", "config", "bundle"):
        data = reads.input(kind)
        require_core(data, kind)
        # TrialEvidence validates every layout 2 document in the inputs envelope.
        if reads.layout_version == 1:
            try:
                validate_document(kind, data)
            except ContractError as exc:
                raise EvidenceError("invalid_trial_input") from exc
        documents[kind] = data
    run, plan, selection = (documents[k] for k in ("run", "plan", "selection"))
    if (root / "migration.json").exists():
        raise UnsupportedFormat("run.source", "migration.json", ("measured",))
    if (
        run["plan_sha256"] != plan["plan_sha256"]
        or run["experiment_id"] != plan["experiment"]["experiment_id"]
        or run["definition_versions"] != plan["experiment"]["definition_versions"]
    ):
        raise EvidenceError("trial_plan_binding_mismatch")
    if selection["run_id"] != run["run_id"] or selection["trial_id"] != run["trial_id"]:
        raise EvidenceError("trial_selection_identity_mismatch")
    trial = trial_for(plan, run["trial_id"])
    workload = next(
        w for w in plan["experiment"]["workloads"] if w["workload_id"] == trial["workload_id"]
    )
    if workload["protocol"]["kind"] == "duration" and run["relation"] == "resume":
        raise EvidenceError("duration_resume_requires_new_window")
    validate_workload_inputs(
        workload,
        documents["config"],
        documents["bundle"],
        experiment_profile=plan["experiment"].get("profile"),
    )
    if run["execution_mode"] == "single":
        if (
            len(plan["trials"]) != 1
            or len(plan["experiment"]["workloads"]) != 1
            or workload["protocol"]["kind"] != "fixed"
            or workload["repeats"] != 1
        ):
            raise EvidenceError("single_execution_requires_one_fixed_trial")
        if any(workload[k]["sha256"] != selection[k + "_sha256"] for k in ("config", "bundle")):
            raise EvidenceError("single_plan_input_hash_mismatch")
    selected = selection["case_ids"]
    expected = run["resumed_case_ids"] if run["relation"] == "resume" else trial["case_order"]
    if selected != expected or selected != [cid for cid in trial["case_order"] if cid in selected]:
        raise EvidenceError("trial_selection_mismatch")
    if (run["parent_run_id"] is None) != (selection["parent_events_sha256"] is None):
        raise EvidenceError("parent_evidence_binding_missing")
    for name in ("config", "bundle"):
        if selection[name + "_sha256"] != reads.input_digest(name):
            raise EvidenceError("trial_input_hash_mismatch")
    if reads.manifest is not None:
        manifest = reads.manifest
        if any(
            manifest.get(k, run[k]) != run[k]
            for k in ("run_id", "experiment_id", "trial_id", "origin", "kind", "execution_mode")
        ):
            raise EvidenceError("manifest_run_identity_mismatch")
        if manifest["run_id"] != run["run_id"] or not core_files(reads).issubset(manifest["files"]):
            raise EvidenceError("manifest_missing_core_identity")
    return documents, limits, trial, workload


def read_trial(root: Path, *, metadata=None):
    root = root.resolve()
    reads = TrialEvidence(root)
    preload_inputs(reads)
    events, event_tails = reads.jsonl("events.jsonl")
    samples, sample_tails = reads.jsonl("memory.jsonl")
    if reads.manifest is not None and (
        type(reads.manifest) is not dict or type(reads.manifest.get("files")) is not dict
    ):
        raise EvidenceError("invalid_manifest")
    manifest_files = reads.manifest["files"] if reads.manifest is not None else {}
    reads.preload(manifest_files)
    limits = verify_manifest(root, _manifest=reads.manifest, _observed=reads.observed())
    if (root / "requests.jsonl").exists():
        raise UnsupportedFormat("run.source", "requests.jsonl", ("events.jsonl",))
    from inferyard.evidence.request_snapshots import require_current_sources

    require_current_sources(root, manifest_files)
    documents, limits, trial, workload = _inputs(root, reads, limits)
    reads.check_input_errors()
    run, selection = documents["run"], documents["selection"]
    limits += event_tails
    limits.extend(
        name + ":" + tail for name, tails in reads.container_tails.items() for tail in tails
    )
    if metadata is not None:
        metadata.update(
            evidence=reads,
            first_event_utc=events[0].get("utc") if events else None,
            manifest=reads.manifest,
            manifest_sha256=reads.hashes.get("manifest.json"),
            identity=reads.observation("identity") or {},
            environment_start=reads.observation("environment", stage="start") or {},
        )
    cases = {c["case_id"]: c for c in documents["bundle"]["cases"]}
    duration = workload["protocol"] if workload["protocol"]["kind"] == "duration" else None
    requests, clock, stopped = reduce_events(events, run, selection, cases, duration=duration)
    from inferyard.evidence.request_snapshots import verify_message_snapshots

    verify_message_snapshots(root, events, reads)
    from inferyard.evidence.engine_capabilities import bound_capabilities

    capabilities = bound_capabilities(
        root,
        manifest_files if reads.manifest is not None else None,
        reads=reads,
    )
    from inferyard.evidence.fixed_output import verify_execution

    verify_execution(
        root,
        workload,
        documents["config"],
        documents["bundle"],
        events,
        capabilities=capabilities,
        reads=reads,
    )
    from inferyard.evidence.cache_execution import verify_execution as verify_cache_execution

    verify_cache_execution(
        root,
        workload,
        documents["config"],
        documents["bundle"],
        events,
        capabilities=capabilities,
        reads=reads,
    )
    from inferyard.evidence.lab import verify_lab

    lab_verified = verify_lab(root, documents["config"], documents["bundle"], events, reads=reads)
    from inferyard.evidence.native import observation_evidence

    engine_observation = observation_evidence(root, documents["config"], reads=reads)
    if metadata is not None:
        from inferyard.evidence.service_drain import service_drain

        metadata["service_drain"] = service_drain(
            events,
            sealed="events.jsonl" in manifest_files,
            truncated=bool(event_tails),
            observation=engine_observation,
        )
    validate_capacity_stop(documents["plan"]["experiment"], requests, stopped)
    window = (
        duration_summary(duration, selection["case_ids"], events, requests) if duration else None
    )
    engine_verified = capabilities.verified
    for request in requests:
        request["engine_build_verified"] = engine_verified
    limits += sample_tails
    for i, sample in enumerate(samples, 1):
        try:
            require_core(sample, "sample")
            validate_document("sample", sample)
        except ContractError as exc:
            raise EvidenceError("invalid_sample") from exc
        if (
            sample["schema_version"] != SCHEMA_VERSION
            or sample["seq"] != i
            or any(sample[k] != run[k] for k in ("run_id", "trial_id", "experiment_id"))
            or (clock is not None and sample["clock_id"] != clock)
        ):
            raise EvidenceError("sample_trial_identity_mismatch")
    core_summary = trial_summary(run, requests, stopped, limits, window, duration)
    counts = core_summary["counts"]
    complete = core_summary["completeness"] == "complete"
    if run["kind"] == "check":
        limits.append("check_only_no_formal_cases")
    if run["relation"] == "resume":
        limits.append("resume_is_partial_observation_not_complete_trial")
    if run["diagnostic"]:
        limits.append("diagnostic_run_not_formal")
    if stopped != "plan_finished":
        limits.append("run_not_finalized")
    if engine_observation and engine_observation["mode"] == "native":
        limits.extend(
            [
                "native_observation_degraded",
                "engine_internal_drain_unavailable",
                "effective_parameters_not_verified",
                "exact_template_budget_unavailable",
            ]
        )
    summary = dict(
        **({"engine_observation": engine_observation} if engine_observation else {}),
        schema_version=SCHEMA_VERSION,
        run_id=run["run_id"],
        trial_id=run["trial_id"],
        experiment_id=run["experiment_id"],
        protocol_kind=workload["protocol"]["kind"],
        **core_summary,
        execution_parameters={
            "request_timeout_seconds": documents["config"]["execution"]["timeout_seconds"],
            "output_budget_tokens": documents["config"]["generation"]["max_tokens"],
        },
        completion_rate=rate(
            counts["completed"], counts["valid_executed"], len(requests) - counts["valid_executed"]
        ),
        quality={
            "status": "repeated_probe_observations_only",
            "independent_cases": len(selection["case_ids"]),
        }
        if duration
        else summarize_quality(
            [cases[c] for c in selection["case_ids"]], requests, complete=complete
        ),
        performance=summarize_performance(requests),
        limitations=list(dict.fromkeys(limits)),
    )
    evidence = [
        *([reads.log_reference("events.jsonl")] if reads.exists("events.jsonl") else []),
        *(reads.input_reference(kind) for kind in ("run", "plan", "selection", "bundle", "config")),
    ]
    if engine_verified:
        evidence.append(reads.observation_reference("service_properties"))
    summary["metric_observations"] = build_observations(
        run,
        trial["workload_id"],
        [cases[c] for c in selection["case_ids"]],
        requests,
        evidence,
        complete=complete,
        repeated=duration is not None,
    )
    summary["metric_observations"].extend(
        output_budget_observations(
            run, workload, list(cases.values()), requests, evidence, complete=complete
        )
    )
    if window is not None:
        summary["duration"] = window
        summary["metric_observations"].extend(
            latency_drift_observations(
                run, trial["workload_id"], window, cases, evidence, complete=complete
            )
        )
    resource_evidence = [
        *evidence,
        *([reads.log_reference("memory.jsonl")] if reads.exists("memory.jsonl") else []),
    ]
    if observation_sealed(reads, "collector", manifest_files):
        resource_evidence.append(reads.observation_reference("collector"))
    summary["resources"], resource_observations = build_resource_observations(
        run,
        trial["workload_id"],
        requests,
        samples,
        documents["config"],
        resource_evidence,
        complete=complete,
    )
    summary["metric_observations"].extend(resource_observations)
    if duration:
        summary["idle_rss"], idle_observations = idle_rss_observations(
            run,
            trial["workload_id"],
            selection["case_ids"],
            requests,
            samples,
            events,
            documents["config"],
            resource_evidence,
            complete=complete,
        )
        summary["metric_observations"].extend(idle_observations)
    token_counts, token_evidence = {}, list(evidence)
    template_source = (
        "/lab/v1/token-budget"
        if lab_verified
        else "not_observed:native"
        if engine_observation and engine_observation["mode"] == "native"
        else "apply-template+tokenize:add_special,parse_special"
    )
    token_name, budgets = read_budgets(
        root,
        documents["config"],
        selection["case_ids"],
        kind=run["kind"],
        manifest=manifest_files if reads.manifest is not None else None,
        reads=reads,
    )
    if (engine_verified or lab_verified) and token_name is not None:
        if run["kind"] == "run":
            token_counts = bind_token_counts(
                selection["case_ids"],
                budgets,
                workload["output_budget_tokens"],
                source=template_source,
            )
        token_evidence.append(reads.observation_reference("token_budgets"))
    target_check = check_input_target(workload, selection["case_ids"], token_counts)
    summary["input_target_check"] = target_check
    if (engine_verified or lab_verified) and observation_sealed(
        reads, "input_target_check", manifest_files
    ):
        if reads.observation("input_target_check") != target_check:
            raise EvidenceError("input_target_check_recomputation_mismatch")
    summary["input_lengths"], length_observations = input_length_summary(
        run,
        workload,
        requests,
        token_counts,
        token_evidence,
        complete=complete,
        resources=resource_observations,
        source=template_source,
    )
    summary["metric_observations"].extend(length_observations)
    positions, position_observations = position_summary(
        run, workload, requests, token_counts, token_evidence, complete=complete
    )
    if positions is not None:
        summary["positions"] = positions
        summary["metric_observations"].extend(position_observations)
    summary["measurement_context"] = measurement_context(
        root,
        manifest_files,
        documents["config"],
        requests,
        performance_policy=documents["plan"]["experiment"].get("performance_environment"),
        reads=reads,
    )
    if "cache_protocol" in workload:
        from inferyard.analysis.cache_observations import cache_series

        summary["cache_observations"] = cache_series(
            events, documents["config"], requests, build_verified=engine_verified
        )
    # Reducers may use tuple intervals internally; the public summary is JSON data.
    summary = strict_json_loads(json_bytes(summary).decode())
    validate_document("summary", summary)
    return {
        **documents,
        **(
            {"resource_collector": reads.observation("collector")}
            if "resource_comparison" in documents["plan"]["experiment"]
            and observation_sealed(reads, "collector", manifest_files)
            else {}
        ),
        "requests": requests,
        "samples": samples,
        "summary": summary,
        "events_sha256": reads.hashes.get("events.jsonl"),
    }


def read_run_projection(root: Path):
    """Read batch continuation fields without a full offline measurement verification."""
    from inferyard.evidence.run_projection import read_run_projection as project

    return project(root)


def resume_selection(parent):
    """Never retry a failed, cancelled, or tool-invalid attempt implicitly."""
    selected = [r["case_id"] for r in parent["requests"] if r["execution_state"] == "not_executed"]
    if not selected:
        raise EvidenceError("no_unexecuted_cases_to_resume")
    return selected


def plan_progress(plan, trials):
    """A history projection, not a merged score or a continuous performance run."""
    validate_document("plan", plan)
    by_trial = {t["trial_id"]: [] for t in plan["trials"]}
    seen = set()
    for data in trials:
        run = data["run"]
        if (
            run["run_id"] in seen
            or run["plan_sha256"] != plan["plan_sha256"]
            or run["trial_id"] not in by_trial
        ):
            raise EvidenceError("invalid_plan_run_history")
        seen.add(run["run_id"])
        by_trial[run["trial_id"]].append(
            dict(
                run_id=run["run_id"],
                relation=run["relation"],
                completeness=data["summary"]["completeness"],
                counts=data["summary"]["counts"],
            )
        )
    return [
        dict(
            trial_id=tid,
            runs=runs,
            status="not_started"
            if not runs
            else "complete"
            if any(r["completeness"] == "complete" for r in runs)
            else "incomplete",
        )
        for tid, runs in by_trial.items()
    ]
