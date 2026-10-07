"""Own only the control evidence and host lock; the service is externally managed."""

import asyncio
import os

from inferyard.evidence.storage import EvidenceError, Redactor, sha256_file
from inferyard.extensions.complete_trial_control import execute_complete, read_trial_plan
from inferyard.extensions.extension_evidence import ExtensionJournal, reduce_packet, render
from inferyard.extensions.independent_guard import IndependentGuard
from inferyard.implementation_identity import IdentityContext
from inferyard.platforms.identity import static_preflight
from inferyard.registry import adapter_factory
from inferyard.runtime.lock import HostLock
from inferyard.runtime.trial_runner import TrialDependencies


async def live_complete(plan, loaded, out, *, identity_context=None):
    from inferyard.extensions.workflow import digest

    identity_context = identity_context or IdentityContext()
    config, bundle, spec = loaded.config.to_dict(), loaded.bundle.to_dict(), plan["spec"]
    trial_plan = read_trial_plan(spec, config, bundle, digest)
    name = config["endpoint"].get("api_key_env")
    secret = os.environ.get(name) if name else None
    if name and not secret:
        raise EvidenceError("extension_credential_reference_unavailable")
    redactor = Redactor([secret] if secret else [])
    if any(redactor.clean(value) != value for value in (plan, config, bundle, trial_plan)):
        raise EvidenceError("extension_sensitive_frozen_input")
    with HostLock() as lock:
        if lock.state and lock.state.get("dirty"):
            raise EvidenceError("extension_host_dirty_requires_existing_recovery_flow")
        identity, _ = static_preflight(config)
        store, guard, watch = ExtensionJournal(out, redactor), None, None
        adapter_type = adapter_factory(config["engine"]["adapter"])
        try:
            for filename, value in (
                (
                    "run.json",
                    {
                        "kind": "extension_packet.v1",
                        "origin": "live_driver",
                        "evidence_kind": "live",
                        "tool_source_sha256": identity_context.source,
                    },
                ),
                ("plan.json", plan),
                ("config.frozen.json", config),
                ("bundle.json", bundle),
                ("identity.json", identity),
                ("measurement-plan.json", trial_plan),
            ):
                store.snapshot(filename, value)
            guard = IndependentGuard(store.path / "guardian.jsonl", config).start()
            if guard.stopped():
                raise EvidenceError("extension_independent_guard_stopped")
            current = asyncio.current_task()

            async def watch_guard():
                while not guard.stopped():
                    await asyncio.sleep(0.02)
                current.cancel()

            watch = asyncio.create_task(watch_guard())
            arms = await execute_complete(
                spec,
                trial_plan,
                loaded,
                store,
                lock,
                guard,
                dependencies=TrialDependencies(adapter=adapter_type),
                identity_context=identity_context,
            )
            watch.cancel()
            await asyncio.gather(watch, return_exceptions=True)
            guard.close()
            if guard.unsafe.is_set() or guard.process.exitcode != 0:
                raise EvidenceError("extension_safety_stop_or_drain_incomplete")
            packet = {
                "spec": spec,
                "rows": arms,
                "evidence_kind": "live",
                "guard": guard.evidence(),
            }
            summary = reduce_packet(packet)
            store.snapshot("packet.json", packet)
            store.snapshot("summary.json", summary)
            store.text_snapshot("report.html", render(summary))
            children = [
                {"path": str(p.parent.relative_to(store.path)), "manifest_sha256": sha256_file(p)}
                for p in sorted(store.path.glob("observer-arms/*/manifest.json"))
            ]
            store.snapshot("child-evidence.json", children)
            store.seal()
            lock.clean()
            return store.path, summary
        finally:
            if watch:
                watch.cancel()
                await asyncio.gather(watch, return_exceptions=True)
            if guard and guard.process.is_alive():
                guard.close()
            if not store.sealed:
                # Preserve the ordinary dirty/recovery protocol on any uncertain drain.
                adapter = adapter_type(config["endpoint"]["url"], secret=secret)
                try:
                    drained = await adapter.wait_idle(5)
                except Exception:
                    drained = False
                finally:
                    await adapter.close()
                store.snapshot(
                    "stop.json",
                    {
                        "completed": False,
                        "drain_confirmed": drained,
                        "raw_events_preserved": True,
                        "automatic_retries": 0,
                        "guardian": guard.evidence()
                        if guard and guard.process.exitcode == 0
                        else None,
                    },
                )
                store.seal()
                if drained:
                    lock.clean()
            store.close()
