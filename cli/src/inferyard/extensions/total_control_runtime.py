"""Connect the total ABBA driver to the complete journal/sampler/safety path."""

import asyncio
import hashlib
import time

from inferyard.evidence.storage import json_bytes
from inferyard.extensions.extension_evidence import ExtensionJournal
from inferyard.extensions.total_observer_control import run_total
from inferyard.platforms.identity import environment_snapshot
from inferyard.platforms.resources import ResourceSampler
from inferyard.runtime.safety import SafetyGuard
from inferyard.runtime.safety_trace import SafetyTrace


class Observer:
    def __init__(self, mode, root, config, bundle, policy):
        self.mode, self.root, self.config, self.bundle, self.policy = (
            mode,
            root,
            config,
            bundle,
            policy,
        )
        self.store = self.sampler = self.task = self.safety_task = None

    async def open(self):
        if self.mode == "off":
            return
        self.store = ExtensionJournal(self.root)
        self.store.snapshot("config.frozen.json", self.config)
        self.store.snapshot("bundle.json", self.bundle)
        start = environment_snapshot()
        self.store.snapshot("environment.start.json", start)
        self.sampler = ResourceSampler(self.store, self.config)
        self.sampler.use_start_environment(start)
        self.safety = SafetyGuard(self.policy, self.config, environment_snapshot)
        self.trace = SafetyTrace(self.store, self.policy["interval_seconds"])
        self.trace.check(self.safety.check, self.safety)
        self.task = asyncio.create_task(self.sampler.run())
        self.safety_task = asyncio.create_task(self.safety_loop())

    async def safety_loop(self):
        while not self.sampler.stopped:
            await asyncio.sleep(self.policy["interval_seconds"])
            self.trace.check(lambda: self.safety.check(periodic=True), self.safety, periodic=True)

    def emit(self, kind, data):
        if self.store:
            self.store.event(kind, "formal", data.get("request_id"), data)

    def before(self, row):
        if self.store:
            if self.task.done():
                self.task.result()
            if self.safety_task.done():
                self.safety_task.result()
            self.trace.check(self.safety.check, self.safety)
            self.sampler.set_phase("formal", row["request_id"])
            self.sampler.before_request()
            self.sampler.boundary("request_start")
            self.emit("request_started", row)

    def after(self, row):
        if self.store:
            self.sampler.boundary("request_end")
            self.trace.check(self.safety.check, self.safety)
            self.emit("request_finished", row)

    async def close(self):
        if not self.store:
            return
        try:
            if self.sampler:
                self.sampler.stopped = True
            for task in (self.task, self.safety_task):
                if task and not task.done():
                    task.cancel()
            results = await asyncio.gather(
                *(t for t in (self.task, self.safety_task) if t), return_exceptions=True
            )
            for result in results:
                if isinstance(result, Exception) and not isinstance(result, asyncio.CancelledError):
                    raise result
            self.store.snapshot("environment.end.json", environment_snapshot())
            self.store.flush(sync=True)
            self.store.seal()
        finally:
            self.store.close()


async def execute_total(spec, adapter, root, config, bundle, guard, sink):
    async def workload(observer):
        adapter.record = observer.emit
        # Equal three warmups in each arm; they consume the full-lifecycle budget
        # but never enter the per-case request comparison denominator.
        first = spec["case_ids"][0]
        for _ in range(config["execution"]["warmup_count"]):
            if guard.stopped():
                raise RuntimeError("independent_guard_stopped")
            await adapter.infer(first, 0)
            if not await adapter.wait_idle(0, config["execution"]["idle_wait_seconds"]):
                raise RuntimeError("total_control_warmup_not_drained")
        rows = []
        for i, case in enumerate(spec["case_ids"]):
            if guard.stopped():
                break
            row = {"case_id": case, "request_id": f"control-{i}"}
            started = time.monotonic_ns()
            observer.before(row)
            result = await adapter.infer(case, 0)
            if not await adapter.wait_idle(0, config["execution"]["idle_wait_seconds"]):
                raise RuntimeError("total_control_slot_not_drained")
            output = {k: result.get(k) for k in ("answer", "usage", "state")}
            row.update(
                state=result["state"], output_sha256=hashlib.sha256(json_bytes(output)).hexdigest()
            )
            observer.after(row)
            row["duration_ns"] = time.monotonic_ns() - started
            rows.append(row)
        return rows

    return await run_total(
        spec,
        workload,
        lambda mode: Observer(mode, root / "observer-arms", config, bundle, guard.policy),
        guard,
        arm_sink=sink,
    )
