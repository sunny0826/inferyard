"""Control-plane RPC routing onto local node capabilities."""

from __future__ import annotations

import asyncio
from dataclasses import asdict
from pathlib import Path

from inferyard.contracts import node_protocol as protocol
from inferyard.node.report_export import export_report, list_reports


class RPCError(RuntimeError):
    def __init__(self, code: str, message: str, details: dict | None = None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.details = details

    def as_dict(self) -> dict:
        return {
            "code": self.code,
            "message": self.message,
            **({"details": self.details} if self.details else {}),
        }


class NodeRPC:
    def __init__(self, state_dir: Path, supervisor, jobs):
        self.state_dir = state_dir
        self.supervisor = supervisor
        self.jobs = jobs
        self.workspace = state_dir / "workspace"
        self.workspace.mkdir(parents=True, exist_ok=True)

    async def call(self, method: str, params: dict):
        try:
            return await self._call(method, params)
        except RPCError:
            raise
        except (KeyError, TypeError, ValueError) as exc:
            raise RPCError("invalid_params", str(exc) or "invalid RPC parameters") from exc
        except (OSError, RuntimeError) as exc:
            raise RPCError("operation_failed", str(exc) or type(exc).__name__) from exc

    async def _call(self, method: str, params: dict):
        if method == protocol.METHOD_NODE_DEVICE_CHECK:
            return await self._device_check(params)
        if method == protocol.METHOD_NODE_ASSETS_SCAN:
            return await self._assets_scan(params)
        if method == protocol.METHOD_NODE_ENGINE_INSTALL:
            return await self._engine_install(params)
        if method == protocol.METHOD_SERVICE_LIST:
            return {"services": self.supervisor.list()}
        if method == protocol.METHOD_SERVICE_START:
            return await self.supervisor.start(params)
        if method == protocol.METHOD_SERVICE_STOP:
            return await self.supervisor.stop(
                params["service_id"], timeout=float(params.get("timeout_seconds", 5))
            )
        if method == protocol.METHOD_SERVICE_LOGS:
            return self.supervisor.logs(
                params["service_id"],
                limit=int(params.get("limit_bytes", 65536)),
                max_lines=int(params["max_lines"]) if params.get("max_lines") else None,
            )
        if method == protocol.METHOD_JOB_LIST:
            return {"jobs": self.jobs.list()}
        if method == protocol.METHOD_JOB_START:
            return await self.jobs.start(params)
        if method == protocol.METHOD_JOB_CANCEL:
            return await self.jobs.cancel(
                params["job_id"],
                force=params.get("force") is True,
                drain_timeout=float(params.get("drain_timeout_seconds", 30)),
            )
        if method == protocol.METHOD_JOB_PROGRESS:
            return self.jobs.progress(params["job_id"])
        if method == protocol.METHOD_REPORT_LIST:
            return {"reports": list_reports(self.state_dir / "reports")}
        if method == protocol.METHOD_REPORT_EXPORT:
            report_path = params.get("report_path")
            report_id = params.get("remote_report_id") or params.get("report_id")
            if not report_path and not params.get("run_path") and isinstance(report_id, str):
                if "/" in report_id or "\\" in report_id or report_id in ("", ".", ".."):
                    raise RPCError("invalid_params", "invalid report_id")
                report_path = str(self.state_dir / "reports" / report_id)
            return await asyncio.to_thread(
                export_report,
                state_dir=self.state_dir,
                run_path=params.get("run_path"),
                report_path=report_path,
                out=params.get("out"),
            )
        if method == protocol.METHOD_WORKSPACE_CLEAN:
            return await asyncio.to_thread(self._clean, params)
        raise RPCError("method_not_found", method)

    async def _device_check(self, params: dict) -> dict:
        from inferyard.platforms.device_preflight import inspect_device

        roots = [Path(item) for item in params.get("models_roots", [])]
        model = Path(params["model_path"]) if params.get("model_path") else None
        return await asyncio.to_thread(
            inspect_device,
            roots or [self.workspace / "models"],
            model_path=model,
            disk_path=self.state_dir,
        )

    async def _assets_scan(self, params: dict) -> dict:
        from inferyard.platforms.asset_scan import scan_assets

        return await asyncio.to_thread(
            scan_assets,
            [Path(item) for item in params.get("models_roots", [])],
            [Path(item) for item in params.get("engine_roots", [])],
        )

    async def _engine_install(self, params: dict) -> dict:
        from inferyard.application.engine_prepare import execute
        from inferyard.application.types import CommandRequest

        request = CommandRequest(
            "engine prepare",
            engine_source_url=params["source"],
            out=Path(params["out"]),
            expected_sha256=params.get("expected_sha256"),
            runtime_profile=params.get("runtime_profile"),
            runtime_source_url=params.get("runtime_source_url"),
            runtime_expected_sha256=params.get("runtime_expected_sha256"),
            engine_timeout_seconds=float(params.get("timeout_seconds", 1800)),
            progress_stream=False,
            resume_transfer=params.get("resume") is True,
            proxy_origin=params.get("proxy_origin"),
        )
        code, result = await asyncio.to_thread(execute, request)
        if code != 0:
            raise RPCError("engine_install_failed", ",".join(result.limitations))
        return asdict(result)

    def _clean(self, params: dict) -> dict:
        from inferyard.platforms.asset_cleanup import delete_asset_safely

        target = Path(params["target"]).absolute()
        return delete_asset_safely(
            target,
            allowed_roots=[self.workspace],
            protected_roots=[self.state_dir / "jobs", self.state_dir / "services"],
        )
