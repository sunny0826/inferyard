"""Conservative serial diagnostics for externally managed local model services."""

import asyncio
import os
import platform
import re
import shutil
import time
import uuid
from pathlib import Path
from urllib.parse import urlsplit

from inferyard.adapters.engine_fit import FitClient, FitTransportError
from inferyard.application.types import CommandResult
from inferyard.config.engine_fit import load_plan, request_rows
from inferyard.config.engine_fit_assets import runtime_manifest as _manifest
from inferyard.contracts.validation import ContractError
from inferyard.evidence.engine_fit_checkpoint import request_counts, write_checkpoint
from inferyard.evidence.storage import EvidenceError, Redactor, atomic_bytes, json_bytes
from inferyard.platforms.engine_fit import (
    bind_service,
    check_service,
    host_identity,
    resource_snapshot,
)
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.sensors_linux import LinuxSensors
from inferyard.provenance import tool_source_hash
from inferyard.reporting.engine_fit import seal_run
from inferyard.runtime.lock import HostLock
from inferyard.runtime.signals import install_termination_handler

LIMITATIONS = [
    "diagnostic_requests_not_formal_benchmark_scores",
    "no_independent_effective_parameters_or_template_verification",
    "engine_version_is_server_reported_not_full_dependency_identity",
    "model_binding_is_startup_directory_not_observed_gpu_residency",
    "client_latency_without_observer_overhead_qualification",
    "resource_boundary_samples_not_request_peaks",
    "process_tree_rss_may_double_count_shared_pages",
    "gpu_memory_and_energy_not_collected",
    "no_winner_or_speedup_claim",
]


def _key(request):
    if request.api_key_env is None:
        return None
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", request.api_key_env):
        raise ContractError("api_key_env", "invalid environment variable reference")
    key = os.environ.get(request.api_key_env)
    if not key or any(c in key for c in "\r\n"):
        raise PreflightError("api_key_environment_unavailable")
    return key


class Guard:
    def __init__(self, binding, plan, out):
        self.binding, self.parameters, self.out = binding, plan["parameters"], out
        if platform.system() == "Darwin":
            from inferyard.platforms.sensors_macos import MacSensors

            self.sensors = MacSensors()
        elif platform.system() == "Windows":
            from inferyard.platforms.engine_fit_windows_temperature import WindowsTemperature

            self.sensors = WindowsTemperature()
        else:
            self.sensors = LinuxSensors()
        self.sensors.sources = [
            s for s in self.sensors.sources if s["metric_name"] == "temperature"
        ]
        self.last_sample = None

    def check(self, phase):
        self.last_sample = None
        check_service(self.binding)
        sample = resource_snapshot(self.binding)
        temperature = self.sensors.collect("engine_fit", None)
        self.last_sample = {
            **sample,
            "phase": phase,
            "disk_free_bytes": shutil.disk_usage(self.out).free,
            "temperature_samples": temperature,
            "temperature_missing_reason": None
            if any(s["value"] is not None for s in temperature)
            else "no_verified_temperature_source",
        }
        available = sample["memory_available_bytes"]
        memory_threshold = self.parameters["min_free_memory_bytes"]
        if memory_threshold is not None:
            if available is None:
                raise PreflightError("system_memory_unavailable")
            if available < memory_threshold:
                raise PreflightError("memory_safety_threshold_reached")
        if self.last_sample["disk_free_bytes"] < self.parameters["min_free_disk_bytes"]:
            raise PreflightError("disk_safety_threshold_reached")
        threshold = self.parameters["max_temperature_celsius"]
        if threshold is not None and any(
            s["value"] is not None and s["value"] >= threshold for s in temperature
        ):
            raise PreflightError("temperature_safety_threshold_reached")
        return self.last_sample


async def _guarded_completion(client, prompt, parameters, guard):
    task = asyncio.create_task(
        client.complete(prompt, parameters["max_tokens"], parameters["request_timeout_seconds"])
    )
    checking = None
    try:
        while not task.done():
            done, _ = await asyncio.wait({task}, timeout=0.5)
            if not done:
                # Native identity observation must not block the HTTP deadline or cancellation.
                checking = asyncio.create_task(asyncio.to_thread(guard.check, "in_flight"))
                await asyncio.shield(checking)
        return await task
    finally:
        if not task.done():
            task.cancel()
        # Also consume an already-failed HTTP task when native validation failed first.
        await asyncio.gather(task, return_exceptions=True)
        if checking is not None:
            # A cancelled await cannot stop a native read. Drain its bounded observation
            # before another check or evidence sealing touches guard.last_sample.
            await asyncio.gather(checking, return_exceptions=True)


async def _completed_idle(client, guard):
    """Allow asynchronous gauges to catch up only after our protocol completed."""
    deadline = time.monotonic() + 10
    while True:
        guard.check("draining")
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise PreflightError("engine_not_idle_after_completion")
        try:
            async with asyncio.timeout(remaining):
                observed = await client.idle()
        except TimeoutError as exc:
            raise PreflightError("engine_not_idle_after_completion") from exc
        if observed["idle"]:
            return observed
        await asyncio.sleep(min(0.5, max(0, deadline - time.monotonic())))


def _snapshot(out, rows):
    atomic_bytes(out / "requests.json", json_bytes(rows), overwrite=True)


async def _run(request, plan, key, lock):
    if host_identity()["sha256"] != plan["host"]["sha256"]:
        raise PreflightError("engine_fit_host_changed")
    if _manifest(plan) != plan["model"]:
        raise PreflightError("engine_fit_model_changed")
    options = {}
    if request.fit_engine == "lmstudio":
        options = {
            "lms_path": request.fit_lms_path,
            "models_root": request.fit_models_root,
            "served_model": request.fit_served_model,
        }
    binding = bind_service(
        request.fit_engine,
        Path(plan["model"]["path"]),
        request.server_pid,
        request.endpoint_url,
        **options,
    )
    endpoint = {
        "url": binding["origin"],
        "server_pid": binding["pid"],
        "process_start_ticks": binding["start_ticks"],
    }
    recovering = bool(lock.state and lock.state["dirty"])
    if recovering:
        lock.verify_manual_recovery(request.recovery_confirm, request.recovery_note, endpoint)
    elif request.recovery_confirm:
        raise PreflightError("unexpected_recovery_confirmation")
    redactor = Redactor([key] if key else [])
    # Reject a secret inside immutable inputs instead of changing their frozen hashes.
    if (
        redactor.clean(plan) != plan
        or redactor.clean(request.fit_served_model) != request.fit_served_model
    ):
        raise PreflightError("credential_in_frozen_input")
    client_options = {}
    if request.fit_engine == "lmstudio":
        from inferyard.platforms.engine_fit_lmstudio import LMStudioObserver

        client_options["observer"] = LMStudioObserver(binding)
    async with FitClient(
        request.fit_engine, binding["origin"], request.fit_served_model, key, **client_options
    ) as client:
        pinned_origin = getattr(client, "origin", None)
        if (
            isinstance(pinned_origin, str)
            and urlsplit(pinned_origin).hostname != binding["address"]
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        try:
            service = await client.inspect()
            idle = await client.idle()
        except FitTransportError as exc:
            raise PreflightError(exc.reason) from exc
        if not idle["idle"]:
            raise PreflightError("engine_not_idle")
        check_service(binding)
        if recovering:
            lock.clean()
        # Out must not be in the model tree: snapshots would change the frozen model.
        request.out.parent.mkdir(parents=True, exist_ok=True)
        guard = Guard(binding, plan, request.out.parent)
        initial_sample = guard.check("before_run")
        request.out.mkdir(mode=0o700)
        guard.out = request.out
        rows = request_rows(plan)
        atomic_bytes(request.out / "plan.json", json_bytes(plan))
        _snapshot(request.out, rows)
        run = {
            "schema_version": 3,
            "definition": "engine_fit_run.v2"
            if platform.system() == "Darwin"
            else "engine_fit_run.v1",
            "run_id": uuid.uuid4().hex,
            "plan_id": plan["plan_id"],
            "engine": request.fit_engine,
            "diagnostic": True,
            "performance_comparison_qualified": False,
            "completeness": "incomplete",
            "stop_reason": None,
            "binding": binding,
            "service": redactor.clean(service),
            "resources": [initial_sample],
            "counts": {},
            "limitations": list(LIMITATIONS),
        }
        run["service"]["measurement_source_sha256"] = tool_source_hash()
        if plan["definition"] != "engine_fit_plan.v1":
            run["definition"] = {
                "engine_fit_plan.v2": "engine_fit_run.v3",
                "engine_fit_plan.v3": "engine_fit_run.v4",
                "engine_fit_plan.v4": "engine_fit_run.v5",
            }[plan["definition"]]
            run["platform"] = platform.system()
            run["service"].setdefault("version_missing_reason", None)
        if platform.system() == "Windows":
            from inferyard.config.engine_fit_native_sources import WINDOWS_LIMITATIONS

            run["definition"] = "engine_fit_run.v6"
            run["limitations"].remove(
                "model_binding_is_startup_directory_not_observed_gpu_residency"
            )
            run["limitations"].extend(WINDOWS_LIMITATIONS)
        if plan["parameters"].get("temperature_stop_override_reason") is not None:
            run["limitations"].append("temperature_stop_explicitly_disabled")
        if plan["parameters"].get("memory_stop_override_reason") is not None:
            run["limitations"].append("memory_stop_explicitly_disabled")
        if request.fit_engine == "lmstudio":
            run["limitations"].extend(
                [
                    "model_binding_uses_lms_loaded_path_not_weight_residency",
                    "lmstudio_effective_load_config_and_non_gguf_auxiliary_assets_"
                    "not_independently_verified",
                ]
            )
        if platform.system() == "Darwin":
            run["limitations"].append(
                "process_architecture_and_metal_backend_not_independently_verified"
            )
        run["service"]["idle_before"] = redactor.clean(idle)
        write_checkpoint(request.out, plan, run, rows)
        code = 0
        prompts = {case["id"]: case["prompt"] for case in plan["cases"]}
        current = None
        try:
            for row in rows:
                current = row
                sample = guard.check("before:" + row["request_id"])
                run["resources"].append(sample)
                idle_before = await client.idle()
                sample["idle"] = redactor.clean(idle_before)
                if not idle_before["idle"]:
                    raise PreflightError("engine_not_idle")
                row.update(status="invalid", reason="request_in_flight")
                _snapshot(request.out, rows)
                lock.dirty(run["run_id"], endpoint, row["request_id"], kind="engine-fit")
                response = await _guarded_completion(
                    client, prompts[row["case_id"]], plan["parameters"], guard
                )
                row.update(status="completed", reason=None, response=redactor.clean(response))
                if redactor.changed and "output_credential_redacted" not in run["limitations"]:
                    run["limitations"].append("output_credential_redacted")
                _snapshot(request.out, rows)
                # Only a complete terminal protocol permits clearing our own request.
                check_service(binding)
                idle_after = await _completed_idle(client, guard)
                lock.clean()
                run["resources"].append(
                    {
                        **guard.check("after:" + row["request_id"]),
                        "idle": redactor.clean(idle_after),
                    }
                )
                current = None
            if _manifest(plan) != plan["model"]:
                raise PreflightError("engine_fit_model_changed")
        except asyncio.CancelledError:
            code, run["stop_reason"] = 130, "cancelled"
            if current and current["reason"] == "request_in_flight":
                current.update(status="cancelled", reason="cancelled")
        except FitTransportError as exc:
            code, run["stop_reason"] = 3, exc.reason
            if current and current["reason"] == "request_in_flight":
                current.update(status="failed", reason=exc.reason)
        except PreflightError as exc:
            code, run["stop_reason"] = 3, str(exc)
            if current and guard.last_sample is not None:
                run["resources"].append(
                    {**guard.last_sample, "phase": "stop:" + current["request_id"]}
                )
            if current and current["reason"] == "request_in_flight":
                current.update(status="cancelled", reason=str(exc))
        except EvidenceError, OSError:
            # No further traffic, and the last durable request marker stays dirty.
            code, run["stop_reason"] = 4, "evidence_or_io_error"
            if current and current["reason"] == "request_in_flight":
                current.update(status="invalid", reason="evidence_or_io_error")
        for row in rows:
            if row["status"] == "not_executed" and run["stop_reason"]:
                row["reason"] = run["stop_reason"]
        run["counts"] = request_counts(rows)
        run["completeness"] = "complete" if code == 0 else "incomplete"
        if lock.state and lock.state["dirty"]:
            run["limitations"].append("dirty_service_requires_manual_restart_and_recovery")
        try:
            seal_run(request.out, plan, run, rows)
        except EvidenceError, OSError:
            if not lock.state or not lock.state["dirty"]:
                lock.dirty(run["run_id"], endpoint, rows[-1]["request_id"], kind="engine-fit")
            raise
        details = {
            "counts": run["counts"],
            "stop_reason": run["stop_reason"],
            "performance_comparison_qualified": False,
        }
        if lock.state and lock.state["dirty"]:
            details["recovery_token"] = lock.state["dirty_token"]
        return code, CommandResult(
            request.command,
            "completed" if code == 0 else "stopped",
            run["completeness"],
            run_id=run["run_id"],
            evidence_dir=str(request.out),
            limitations=tuple(run["limitations"]),
            details=details,
        )


async def _with_signals(request, plan, key, lock):
    restore = install_termination_handler()
    try:
        return await _run(request, plan, key, lock)
    finally:
        restore()


def execute(request):
    if platform.system() not in ("Linux", "Darwin", "Windows"):
        raise PreflightError("engine_fit_live_requires_supported_native_platform")
    if platform.system() == "Windows" and request.fit_engine != "llama-cpp":
        raise PreflightError("engine_fit_windows_engine_unavailable")
    if request.fit_engine == "ollama":
        raise PreflightError("ollama_service_idle_observation_unavailable")
    if request.fit_engine in ("mlx-lm", "lmstudio") and platform.system() != "Darwin":
        raise PreflightError("engine_fit_engine_requires_native_macos")
    if request.fit_engine == "lmstudio":
        if request.fit_lms_path is None or request.fit_models_root is None:
            raise PreflightError("lmstudio_observer_paths_required")
    elif request.fit_lms_path is not None or request.fit_models_root is not None:
        raise PreflightError("lmstudio_observer_options_wrong_engine")
    plan = load_plan(request.frozen_plan)
    if platform.system() == "Windows" and (
        plan["definition"] == "engine_fit_plan.v1" or plan["model"].get("kind") != "gguf"
    ):
        raise PreflightError("engine_fit_windows_single_gguf_required")
    if request.fit_engine not in plan["engines"]:
        raise PreflightError("engine_not_in_frozen_plan")
    if request.out.exists() or request.out.is_symlink():
        raise PreflightError("output_directory_exists")
    if request.out.resolve().is_relative_to(Path(plan["model"]["path"]).resolve()):
        raise PreflightError("output_inside_model_directory")
    if not request.fit_served_model or len(request.fit_served_model) > 512:
        raise PreflightError("invalid_served_model")
    key = _key(request)
    with HostLock() as lock:
        return asyncio.run(_with_signals(request, plan, key, lock))
