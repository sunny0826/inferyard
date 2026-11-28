"""Control-plane RPC routing onto local node capabilities."""

from __future__ import annotations

import asyncio
import os
import shutil
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


def _check_cleanup_tree(path: Path) -> None:
    if path.is_symlink():
        raise ValueError("workspace_cleanup_symlink_refused")
    if not path.exists() or not path.is_dir():
        return
    for root, directories, files in os.walk(path, followlinks=False):
        for name in (*directories, *files):
            if (Path(root) / name).is_symlink():
                raise ValueError("workspace_cleanup_symlink_refused")


def _remove_managed(path: Path) -> str | None:
    if not path.exists() and not path.is_symlink():
        return None
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path)
    else:
        path.unlink()
    return str(path)


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
            return await self.jobs.start_request(params)
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

        source = params.get("source_url") or params.get("source")
        if not isinstance(source, str) or not source:
            raise ValueError("engine source URL is required")
        out = params.get("out")
        if out:
            destination = Path(out)
        else:
            family, release = params["family"], params["release_id"]
            allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
            if not all(
                isinstance(value, str) and value and set(value) <= set(allowed)
                for value in (family, release)
            ):
                raise ValueError("invalid engine family or release_id")
            destination = self.workspace / "engines" / f"{family}-{release}"
        request = CommandRequest(
            "engine prepare",
            engine_source_url=source,
            out=destination,
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

    def _managed_clean(self) -> dict:
        targets = []
        for record in self.jobs.list():
            state = record.get("state") or record.get("status")
            job_id = record.get("job_id") or record.get("task_id")
            if state not in ("running", "starting") and isinstance(job_id, str):
                if not job_id or set(job_id) - set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"):
                    raise ValueError("invalid job_id")
                targets.append(self.state_dir / "jobs" / job_id / "results")
        reports = self.state_dir / "reports"
        _check_cleanup_tree(reports)
        if reports.is_dir():
            targets.extend(path for path in reports.glob("export-*"))
        for name in ("candidates", "bound", "results", "reports"):
            root = self.workspace / name
            _check_cleanup_tree(root)
            if root.exists() and not root.is_dir():
                raise ValueError("workspace_cleanup_invalid_root")
            if root.is_dir():
                targets.extend(root.iterdir())
        targets = list(dict.fromkeys(targets))
        for target in targets:
            _check_cleanup_tree(target)
        removed = [value for target in targets if (value := _remove_managed(target)) is not None]
        return {"removed": removed}

    def _clean(self, params: dict) -> dict:
        if not params.get("target"):
            return self._managed_clean()
        from inferyard.platforms.asset_cleanup import delete_asset_safely

        target = Path(params["target"]).absolute()
        return delete_asset_safely(
            target,
            allowed_roots=[self.workspace],
            protected_roots=[self.state_dir / "jobs", self.state_dir / "services"],
        )
