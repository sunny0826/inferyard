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
from inferyard.node.service_supervisor import terminate_tree


def _write_json(path: Path, value: dict) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")
    os.replace(temporary, path)


class JobRunner:
    def __init__(self, state_dir: Path, supervisor, *, event_sink=None, executor=None):
        self.root = state_dir / "jobs"
        self.root.mkdir(parents=True, exist_ok=True)
        self.supervisor = supervisor
        self.event_sink = event_sink
        self.executor = executor
        self.jobs: dict[str, dict] = {}
        self.tasks: dict[str, asyncio.Task] = {}
        for path in self.root.glob("*/job.json"):
            try:
                record = json.loads(path.read_text(encoding="utf-8"))
                if isinstance(record.get("job_id"), str):
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

    def _request(self, job_id: str, params: dict) -> tuple[CommandRequest, dict]:
        from inferyard.config.loader import load_config

        config_path = params.get("config") or params.get("config_path")
        if not isinstance(config_path, str):
            raise ValueError("job config path is required")
        loaded = load_config(config_path)
        config = loaded.config.to_dict()
        service_id = params.get("service_id")
        if service_id:
            endpoint = self.supervisor.endpoint(service_id)
        else:
            endpoint = dict(config["endpoint"])
            parsed = urlsplit(endpoint.get("url", ""))
            if parsed.scheme != "http" or parsed.hostname != "127.0.0.1":
                raise ValueError("remote jobs require a loopback endpoint")
        config["endpoint"].update(endpoint)
        config["output"]["root"] = str(self.root / job_id / "results")
        loaded = type(loaded)(
            loaded.source,
            Document.parse("config", config),
            loaded.bundle,
            source_data=loaded.source_data,
            source_media_type=loaded.source_media_type,
            source_missing_reason=loaded.source_missing_reason,
        )
        frozen = params.get("frozen_plan")
        request = CommandRequest(
            "run",
            config=loaded,
            frozen_plan=Path(frozen) if frozen else None,
            output_root=self.root / job_id / "results",
        )
        return request, endpoint

    async def start(self, params: dict) -> dict:
        job_id = params.get("job_id") or uuid.uuid4().hex
        if not isinstance(job_id, str) or not job_id or job_id in self.jobs:
            raise ValueError("invalid or duplicate job_id")
        if any(character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-" for character in job_id):
            raise ValueError("invalid job_id")
        job_dir = self.root / job_id
        job_dir.mkdir(parents=True, exist_ok=False)
        request, endpoint = self._request(job_id, params)
        record = {
            "job_id": job_id,
            "status": "running",
            "code": None,
            "created_at": datetime.now(UTC).isoformat(),
            "evidence_dir": None,
            "worker_pid": endpoint.get("server_pid"),
            "worker_start_ticks": endpoint.get("process_start_ticks"),
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
            code, result = await executor(request)
            record.update(
                code=code,
                status="finished" if code == 0 else "cancelled" if code == 130 else "blocked",
                evidence_dir=result.evidence_dir,
                result=asdict(result),
            )
        except asyncio.CancelledError:
            record.update(code=130, status="cancelled")
        except Exception as exc:
            record.update(code=4, status="error", error=type(exc).__name__)
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
                "requested_at": datetime.now(UTC).isoformat(),
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
        if task.done() and record["status"] == "running":
            record["status"] = "cancelled"
            record["code"] = 130
        if timed_out:
            pid, ticks = record.get("worker_pid"), record.get("worker_start_ticks")
            if type(pid) is int and type(ticks) is int:
                forced = await asyncio.to_thread(terminate_tree, pid, ticks, graceful_timeout=0)
                record["forced_termination_reason"] = forced or "cooperative_drain_timeout"
            else:
                record["forced_termination_reason"] = "cooperative_drain_timeout"
            record["status"] = "cancelled"
            record["code"] = 130
            self._save(record)
        record["cancel_requested"] = True
        record["cancel_completed_at"] = time.time()
        self._save(record)
        await self._event(record)
        return dict(record)

    async def close(self) -> None:
        for task in self.tasks.values():
            task.cancel()
        await asyncio.gather(*self.tasks.values(), return_exceptions=True)
