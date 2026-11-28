"""Remote benchmark jobs using the existing runner, HostLock and cancellation finalization."""

from __future__ import annotations

import asyncio
import json
import os
import time
import uuid
from dataclasses import asdict
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import urlsplit

from inferyard.application.types import CommandRequest
from inferyard.contracts.node_protocol import CANCEL_MARKER_FILENAME, EVENT_JOB_UPDATED
from inferyard.contracts.validation import Document


def _timestamp() -> str:
    return datetime.now(UTC).isoformat()


def _write_json(path: Path, value: dict) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")
    os.replace(temporary, path)


def _validate_loopback(url: str) -> None:
    parsed = urlsplit(url)
    if parsed.scheme != "http" or parsed.hostname != "127.0.0.1":
        raise ValueError("remote jobs require a loopback endpoint")


def _validate_frozen_plan(plan_path: Path, expected: dict | None) -> None:
    from inferyard.config.plan_inputs import read_frozen_plan

    _, workloads = read_frozen_plan(plan_path)
    for loaded in workloads.values():
        endpoint = loaded.config.to_dict()["endpoint"]
        url = endpoint.get("url", "")
        _validate_loopback(url)
        if expected is not None:
            if url != expected.get("url") or endpoint.get("server_pid") != expected.get(
                "server_pid"
            ):
                raise ValueError("frozen plan endpoint does not match expected service binding")


def _set_state(record: dict, state: str) -> None:
    record["state"] = state
    record["status"] = state


class JobRunner:
    def __init__(self, state_dir: Path, supervisor, *, event_sink=None, executor=None):
        self.root = state_dir / "jobs"
        self.root.mkdir(parents=True, exist_ok=True)
        self.supervisor = supervisor
        self.event_sink = event_sink
        self.executor = executor
        self.jobs: dict[str, dict] = {}
        self.tasks: dict[str, asyncio.Task] = {}
        self._exec_lock = asyncio.Lock()
        for path in self.root.glob("*/job.json"):
            try:
                record = json.loads(path.read_text(encoding="utf-8"))
                if isinstance(record.get("job_id"), str):
                    record.setdefault("task_id", record["job_id"])
                    record.setdefault("state", record.get("status", "failed"))
                    self.jobs[record["job_id"]] = record
            except (OSError, ValueError):
                continue

    def list(self) -> list[dict]:
        return [dict(self.jobs[key]) for key in sorted(self.jobs)]

    def progress(self, job_id: str) -> dict:
        record = dict(self._record(job_id))
        marker = self.root / job_id / CANCEL_MARKER_FILENAME
        record["cancel_requested"] = marker.exists()
        return record

    def _record(self, job_id: str) -> dict:
        try:
            return self.jobs[job_id]
        except KeyError as exc:
            raise ValueError("unknown job_id") from exc

    def _save(self, record: dict) -> None:
        _write_json(self.root / record["job_id"] / "job.json", record)

    async def _event(self, record: dict) -> None:
        if self.event_sink is not None:
            await self.event_sink(EVENT_JOB_UPDATED, dict(record))

    def _endpoint_override(self, params: dict) -> dict | None:
        service_id = params.get("service_id")
        if service_id:
            return self.supervisor.endpoint(service_id)
        endpoint_url = params.get("endpoint_url")
        if endpoint_url is not None:
            _validate_loopback(str(endpoint_url))
            return {
                "url": str(endpoint_url),
                "server_pid": params.get("server_pid"),
            }
        return None

    def _default_running_endpoint(self) -> dict | None:
        list_fn = getattr(self.supervisor, "list", None)
        if not callable(list_fn):
            return None
        for record in list_fn():
            if record.get("status") == "running" and record.get("service_id"):
                try:
                    return self.supervisor.endpoint(record["service_id"])
                except ValueError:
                    continue
        return None

    def _request(self, job_id: str, params: dict) -> CommandRequest:
        action = str(params.get("action") or "start").strip().lower()
        endpoint_override = self._endpoint_override(params)
        output_root = self.root / job_id / "results"
        if action in ("resume", "rerun") and params.get("from_run"):
            from_run = Path(str(params["from_run"]))
            command = "resume" if action == "resume" else "run"
            if action == "rerun" and (
                endpoint_override is None or endpoint_override.get("server_pid") is None
            ):
                endpoint_override = self._default_running_endpoint()
                if endpoint_override is None or endpoint_override.get("server_pid") is None:
                    raise ValueError("running service is required for remote rerun")
            return CommandRequest(
                command,
                from_run=from_run,
                endpoint_url=endpoint_override["url"] if endpoint_override else None,
                server_pid=endpoint_override.get("server_pid") if endpoint_override else None,
                output_root=output_root,
            )

        from inferyard.config.loader import load_config

        config_path = params.get("config") or params.get("config_path")
        if not isinstance(config_path, str):
            raise ValueError("job config path is required")
        loaded = load_config(config_path)
        config = loaded.config.to_dict()
        if endpoint_override is not None:
            endpoint = dict(endpoint_override)
        else:
            endpoint = dict(config["endpoint"])
            _validate_loopback(endpoint.get("url", ""))
        config["endpoint"].update({k: v for k, v in endpoint.items() if v is not None})
        config["output"]["root"] = str(output_root)
        loaded = type(loaded)(
            loaded.source,
            Document.parse("config", config),
            loaded.bundle,
            source_data=loaded.source_data,
            source_media_type=loaded.source_media_type,
            source_missing_reason=loaded.source_missing_reason,
        )
        frozen = params.get("frozen_plan")
        if frozen:
            _validate_frozen_plan(Path(frozen), endpoint_override)
        return CommandRequest(
            "run",
            config=loaded,
            frozen_plan=Path(frozen) if frozen else None,
            output_root=output_root,
        )

    async def start_request(self, params: dict) -> dict:
        action = str(params.get("action") or "start").strip().lower()
        if action in ("resume", "rerun") and isinstance(params.get("remote_run_paths"), list):
            paths = params["remote_run_paths"]
            if not paths:
                raise ValueError("remote_run_paths must not be empty")
            records = []
            for run_path in paths:
                item = {k: v for k, v in params.items() if k not in ("remote_run_paths", "job_id")}
                item["from_run"] = run_path
                records.append(await self.start(item))
            return {"jobs": records}
        if isinstance(params.get("config_paths"), list):
            paths = params["config_paths"]
            if not paths:
                raise ValueError("config_paths must not be empty")
            records = []
            for config_path in paths:
                item = {k: v for k, v in params.items() if k not in ("config_paths", "job_id")}
                item["config_path"] = config_path
                records.append(await self.start(item))
            return {"jobs": records}
        return await self.start(params)

    async def start(self, params: dict) -> dict:
        job_id = params.get("job_id") or uuid.uuid4().hex
        if not isinstance(job_id, str) or not job_id or job_id in self.jobs:
            raise ValueError("invalid or duplicate job_id")
        allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
        if any(character not in allowed for character in job_id):
            raise ValueError("invalid job_id")
        job_dir = self.root / job_id
        job_dir.mkdir(parents=True, exist_ok=False)
        try:
            request = self._request(job_id, params)
        except BaseException:
            try:
                job_dir.rmdir()
            except OSError:
                pass
            raise
        record = {
            "job_id": job_id,
            "task_id": job_id,
            "status": "running",
            "state": "running",
            "code": None,
            "created_at": _timestamp(),
            "finished_at": None,
            "evidence_dir": None,
            "run_path": None,
            "worker_pid": None,
            "worker_start_ticks": None,
            "forced_termination_reason": None,
        }
        self.jobs[job_id] = record
        self._save(record)
        self.tasks[job_id] = asyncio.create_task(self._execute(request, record))
        await self._event(record)
        return dict(record)

    async def _execute(self, request: CommandRequest, record: dict) -> None:
        if self.executor is None:
            from inferyard.application.execution import execute_async

            executor = execute_async
        else:
            executor = self.executor
        try:
            async with self._exec_lock:
                code, result = await executor(request)
            final_state = "done" if code == 0 else "cancelled" if code == 130 else "failed"
            _set_state(record, final_state)
            record.update(
                code=code,
                finished_at=_timestamp(),
                evidence_dir=result.evidence_dir,
                run_path=result.evidence_dir,
                run_id=result.run_id,
                result=asdict(result),
            )
        except asyncio.CancelledError:
            _set_state(record, "cancelled")
            record.update(code=130, finished_at=_timestamp())
        except Exception as exc:
            _set_state(record, "failed")
            record.update(
                code=4,
                finished_at=_timestamp(),
                error={
                    "code": "job_failed",
                    "message": str(exc) or type(exc).__name__,
                    "limitations": [],
                },
            )
        self._save(record)
        await self._event(record)

    async def cancel(
        self,
        job_id: str,
        *,
        force: bool = False,
        drain_timeout: float = 30,
        reason: str = "user_cancelled",
    ) -> dict:
        record = self._record(job_id)
        task = self.tasks.get(job_id)
        marker = self.root / job_id / CANCEL_MARKER_FILENAME
        _write_json(
            marker,
            {
                "schema_version": 1,
                "reason": reason,
                "force": force,
                "requested_at": _timestamp(),
            },
        )
        if task is None or task.done():
            record["cancel_requested"] = True
            self._save(record)
            return dict(record)
        task.cancel()
        timed_out = force
        if not force:
            try:
                await asyncio.wait_for(asyncio.shield(task), timeout=drain_timeout)
            except TimeoutError:
                timed_out = True
            except asyncio.CancelledError:
                pass
        if task.done() and record["state"] == "running":
            _set_state(record, "cancelled")
            record.update(code=130, finished_at=_timestamp())
        if timed_out:
            task.cancel()
            record["forced_termination_reason"] = "cooperative_drain_timeout"
            _set_state(record, "cancelled")
            record.update(code=130, finished_at=_timestamp())
        record["cancel_requested"] = True
        record["cancel_completed_at"] = time.time()
        self._save(record)
        await self._event(record)
        return dict(record)

    async def close(self) -> None:
        for task in self.tasks.values():
            task.cancel()
        await asyncio.gather(*self.tasks.values(), return_exceptions=True)
