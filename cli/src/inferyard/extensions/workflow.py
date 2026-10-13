"""CLI freeze/run/replay for versioned extensions; live use is never implicit."""

import asyncio
import hashlib
import os
from copy import deepcopy

from inferyard.evidence.storage import (
    EvidenceError,
    Redactor,
    atomic_bytes,
    json_bytes,
    read_json,
    sha256_file,
)
from inferyard.extensions.client_capacity import ClientCapacity
from inferyard.extensions.closed_concurrency import run_closed
from inferyard.extensions.closed_concurrency import validate_spec as validate_closed
from inferyard.extensions.extension_evidence import (
    ExtensionJournal,
    reduce_packet,
    render,
    save_packet,
    verify_packet,
)
from inferyard.extensions.extension_transport import ExtensionTransport
from inferyard.extensions.independent_guard import POLICY, IndependentGuard
from inferyard.extensions.native_tools import run_tool_case
from inferyard.extensions.native_tools import validate_spec as validate_tools
from inferyard.extensions.total_control_runtime import execute_total
from inferyard.extensions.total_observer_control import validate_spec as validate_total
from inferyard.platforms.identity import PreflightError, static_preflight
from inferyard.platforms.resources import ResourceSampler
from inferyard.provenance import tool_source_hash
from inferyard.runtime.lock import HostLock

VALIDATORS = {
    "closed_concurrency.v1": validate_closed,
    "native_tools.v1": validate_tools,
    "total_observer_control.v1": validate_total,
    "total_observer_control.v2": validate_total,
}


def digest(data):
    return hashlib.sha256(json_bytes(data)).hexdigest()


async def guarded(coroutine, stopped):
    task, cancel = asyncio.create_task(coroutine), asyncio.create_task(stopped.wait())
    try:
        done, _ = await asyncio.wait((task, cancel), return_when=asyncio.FIRST_COMPLETED)
        if cancel in done and stopped.is_set():
            raise asyncio.CancelledError()
        return task.result()
    finally:
        task.cancel()
        cancel.cancel()
        await asyncio.gather(task, cancel, return_exceptions=True)


def freeze(spec, loaded, out):
    source_identity = tool_source_hash()
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    spec = deepcopy(spec)
    if spec.get("definition") in ("total_observer_control.v1", "total_observer_control.v2"):
        spec.update(
            tool_source_sha256=source_identity,
            config_sha256=digest(config),
            bundle_sha256=digest(bundle),
            guardian_policy_sha256=digest(POLICY),
        )
    validator = VALIDATORS.get(spec.get("definition"))
    if validator is None:
        raise EvidenceError("extension_definition_not_supported")
    validator(spec)
    if spec["definition"] == "total_observer_control.v2":
        from inferyard.extensions.complete_trial_control import read_trial_plan

        read_trial_plan(spec, config, bundle, digest)
    if spec["definition"] != "native_tools.v1" and not set(spec["case_ids"]) <= {
        c["case_id"] for c in bundle["cases"]
    }:
        raise EvidenceError("extension_case_absent_from_reviewed_bundle")
    if spec["definition"] == "native_tools.v1" and spec["track"] != "capability_diagnostic":
        # Formal tool content has a new review boundary: a hash alone is not a
        # recorded human approval. This CLI only executes capability diagnostics.
        raise EvidenceError("formal_tool_scenarios_require_separate_reviewed_bundle")
    plan = {
        "kind": "extension_plan.v1",
        "spec": spec,
        "config_sha256": digest(config),
        "bundle_sha256": digest(bundle),
        "tool_source_sha256": source_identity,
    }
    plan["plan_sha256"] = digest(plan)
    out.mkdir(parents=True, exist_ok=False)
    atomic_bytes(out / "plan.json", json_bytes(plan))
    return plan


def read_plan(path, loaded, *, source_identity=None):
    plan = read_json(path)
    if plan.get("kind") != "extension_plan.v1" or plan.get("plan_sha256") != digest(
        {k: v for k, v in plan.items() if k != "plan_sha256"}
    ):
        raise EvidenceError("extension_plan_hash_mismatch")
    if (
        plan.get("config_sha256") != digest(loaded.config.to_dict())
        or plan.get("bundle_sha256") != digest(loaded.bundle.to_dict())
        or plan.get("tool_source_sha256") != (source_identity or tool_source_hash())
    ):
        raise EvidenceError("extension_frozen_input_or_source_mismatch")
    spec = plan["spec"]
    validator = VALIDATORS.get(spec.get("definition"))
    if validator is None:
        raise EvidenceError("extension_definition_not_supported")
    validator(spec)
    if spec["definition"] == "native_tools.v1" and spec["track"] != "capability_diagnostic":
        raise EvidenceError("formal_tool_scenarios_require_separate_reviewed_bundle")
    return plan


async def live(plan, loaded, out, *, identity_context=None):
    from inferyard.implementation_identity import IdentityContext

    identity_context = identity_context or IdentityContext(source_hash=tool_source_hash)
    if os.name == "nt":
        raise PreflightError("windows_phase2_live_not_supported")
    spec, config, bundle = plan["spec"], loaded.config.to_dict(), loaded.bundle.to_dict()
    if spec["definition"] == "total_observer_control.v2":
        from inferyard.extensions.complete_control_workflow import live_complete

        return await live_complete(plan, loaded, out, identity_context=identity_context)
    secret = os.environ.get(config["endpoint"]["api_key_env"])
    if not secret:
        raise EvidenceError("extension_credential_reference_unavailable")
    redactor = Redactor([secret])
    if (
        redactor.clean(plan) != plan
        or redactor.clean(config) != config
        or redactor.clean(bundle) != bundle
    ):
        raise EvidenceError("extension_sensitive_frozen_input")
    with HostLock() as lock:
        if lock.state and lock.state.get("dirty"):
            raise EvidenceError("extension_host_dirty_requires_existing_recovery_flow")
        identity, _ = static_preflight(config)
        store = ExtensionJournal(out, redactor)
        adapter = guard = guard_task = resource_task = resource_sampler = capacity = None
        stopped, unknown = asyncio.Event(), True

        def emit(kind, data):
            store.event(kind, "formal", data.get("request_id"), data)

        try:
            store.snapshot(
                "run.json",
                {
                    "kind": "extension_packet.v1",
                    "origin": "live_driver",
                    "evidence_kind": "live",
                    "tool_source_sha256": identity_context.source,
                },
            )
            for name, data in (
                ("plan.json", plan),
                ("config.frozen.json", config),
                ("bundle.json", bundle),
                ("identity.json", identity),
            ):
                store.snapshot(name, data)
            concurrency = spec.get("server_slots", 1)
            adapter = ExtensionTransport(
                config,
                bundle["cases"],
                answer_policy=bundle["answer_policy"],
                secret=secret,
                concurrency=concurrency,
                emit=emit,
            )
            await adapter.verify(tools=spec["definition"] == "native_tools.v1")
            guard = IndependentGuard(store.path / "guardian.jsonl", config).start()

            async def watch_guard():
                while not guard.stopped():
                    await asyncio.sleep(0.02)
                stopped.set()

            guard_task = asyncio.create_task(watch_guard())
            if guard.stopped():
                raise EvidenceError("extension_independent_guard_stopped")
            lock.dirty(store.run_id, config["endpoint"], "extension", kind="extension")
            await guarded(adapter.probe_cancellation(), stopped)
            definition = spec["definition"]
            if definition == "closed_concurrency.v1":
                resource_sampler = ResourceSampler(store, config)
                resource_sampler.set_phase("formal", None)

                async def resources():
                    try:
                        await resource_sampler.run()
                    except Exception:
                        stopped.set()
                        raise

                resource_task = asyncio.create_task(resources())
            if definition == "closed_concurrency.v1":
                capacity = ClientCapacity()
                capacity.start()
                try:
                    rows = await run_closed(spec, adapter, emit, stop=stopped)
                finally:
                    capacity_proof = await capacity.close()
                    capacity = None
            elif definition == "native_tools.v1":
                rows = []
                for case in spec["cases"]:
                    if stopped.is_set():
                        row = {
                            "case_id": case["case_id"],
                            "state": "not_executed",
                            "error": "guardian_stopped",
                            "replies": [],
                            "executions": [],
                        }
                        from inferyard.extensions.native_tools import score_tool_case

                        row["checks"] = score_tool_case(case, row)
                    else:

                        async def chat(messages, tools):
                            return await guarded(
                                adapter.completion(messages, tools=tools, stream=True), stopped
                            )

                        row = await run_tool_case(spec, case, chat, emit)
                        if not await adapter.wait_idle(0, 5) or row["state"] == "cancelled":
                            stopped.set()
                    rows.append(row)
            else:

                def sink(arms):
                    emit("control_checkpoint", {"arms": arms})

                class GuardedAdapter:
                    def __getattr__(self, name):
                        return getattr(adapter, name)

                    @property
                    def record(self):
                        return adapter.record

                    @record.setter
                    def record(self, value):
                        adapter.record = value

                    async def infer(self, case, slot):
                        return await guarded(adapter.infer(case, slot), stopped)

                rows = await execute_total(
                    spec, GuardedAdapter(), store.path, config, bundle, guard, sink
                )
            unknown = not all([await adapter.wait_idle(i, 5) for i in range(concurrency)])
            guard_task.cancel()
            await asyncio.gather(guard_task, return_exceptions=True)
            guard.close()
            if guard.unsafe.is_set() or guard.process.exitcode != 0:
                stopped.set()
            if resource_task:
                resource_sampler.stopped = True
                resource_task.cancel()
                result = await asyncio.gather(resource_task, return_exceptions=True)
                if isinstance(result[0], Exception):
                    stopped.set()
            packet = {"spec": spec, "rows": rows, "evidence_kind": "live"}
            if definition == "closed_concurrency.v1":
                packet["client_capacity"] = capacity_proof
            if definition == "total_observer_control.v1":
                packet["guard"] = guard.evidence()
            if stopped.is_set() or unknown:
                raise EvidenceError("extension_safety_stop_or_drain_incomplete")
            summary = reduce_packet(packet)
            store.snapshot("packet.json", packet)
            store.snapshot("summary.json", summary)
            store.text_snapshot("report.html", render(summary))
            # Bind every observer sub-run before sealing the parent packet.
            children = [
                {"path": str(p.parent.relative_to(store.path)), "manifest_sha256": sha256_file(p)}
                for p in sorted(store.path.glob("observer-arms/*/manifest.json"))
            ]
            store.snapshot("child-evidence.json", children)
            store.seal()
            lock.clean()
            return store.path, summary
        finally:
            if capacity:
                await capacity.close()
            if resource_task:
                resource_sampler.stopped = True
                resource_task.cancel()
                await asyncio.gather(resource_task, return_exceptions=True)
            if guard_task:
                guard_task.cancel()
                await asyncio.gather(guard_task, return_exceptions=True)
            if guard and guard.process.is_alive():
                guard.close()
            if adapter:
                if not store.sealed:
                    try:
                        unknown = not all(
                            [await adapter.wait_idle(i, 5) for i in range(adapter.concurrency)]
                        )
                    except Exception:
                        unknown = True
                await adapter.close()
            if not store.sealed:
                store.snapshot(
                    "stop.json",
                    {
                        "completed": False,
                        "drain_confirmed": not unknown,
                        "raw_events_preserved": True,
                        "automatic_retries": 0,
                    },
                )
                store.seal()
                if not unknown:
                    lock.clean()
            store.close()


def execute(request):
    from inferyard.application.types import CommandResult

    if request.command == "extension-check":
        summary = verify_packet(request.run)
        path, status = request.run, "verified"
    elif request.command == "extension-freeze":
        plan = freeze(read_json(request.extension_spec), request.config, request.out)
        return 0, CommandResult(
            request.command, "frozen", "complete", evidence_dir=str(request.out), details=plan
        )
    elif request.command == "extension-replay":
        packet = read_json(request.extension_spec)
        if packet.get("evidence_kind") != "fixture":
            raise EvidenceError("offline_import_cannot_grant_live_qualification")
        path, summary = save_packet(request.out, packet)
        status = "software_verified"
    else:
        from inferyard.implementation_identity import IdentityContext

        identity_context = IdentityContext(source_hash=tool_source_hash)
        plan = read_plan(
            request.extension_spec, request.config, source_identity=identity_context.source
        )
        path, summary = asyncio.run(
            live(plan, request.config, request.out, identity_context=identity_context)
        )
        status = "executed"
    return 0, CommandResult(
        request.command, status, "complete", evidence_dir=str(path), details=summary
    )
