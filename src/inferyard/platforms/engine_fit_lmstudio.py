"""Bound, read-only LM Studio CLI observations; never discover or wake services."""

import asyncio
import os
import time
from pathlib import Path, PurePosixPath

from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms.engine_fit_observer_io import TOTAL_TIMEOUT, read_process, remaining
from inferyard.platforms.identity import PreflightError

_LIMIT = 1024 * 1024
_TIMEOUT = 2.0
_STATES = {"idle", "processingPrompt", "generating", "computingEmbedding"}


def _run(command, *, deadline=None):
    """Bound stdout while running only this short-lived observer child."""
    try:
        deadline = min(
            deadline if deadline is not None else float("inf"),
            time.monotonic() + _TIMEOUT,
        )
        code, output, _ = read_process(
            command,
            deadline=deadline,
            limit=_LIMIT,
            prefix="engine_fit_lms_observer",
        )
        if code != 0:
            raise PreflightError("engine_fit_lms_observer_failed")
        return strict_json_loads(output.decode("utf-8"))
    except (OSError, UnicodeError, ValueError, ContractError) as exc:
        raise PreflightError("engine_fit_lms_observer_unreadable") from exc


def _cli_hash(path):
    from inferyard.platforms.engine_fit import _file_hash

    if not path.is_file() or not os.access(path, os.X_OK):
        raise PreflightError("engine_fit_lms_cli_unavailable")
    return _file_hash(path)[0]


def _query(observer, command, address, port, deadline):
    if address != "127.0.0.1" or type(port) is not int or not 1 <= port <= 65535:
        raise PreflightError("engine_fit_lms_endpoint_unverified")
    remaining(deadline)
    path = Path(observer["path"])
    if _cli_hash(path) != observer["sha256"]:
        raise PreflightError("engine_fit_lms_cli_changed")
    # Omitting host selects local CLI authentication; an explicit port skips discovery/startup.
    result = _run(
        [
            str(path),
            *command,
            "--json",
            "--port",
            str(port),
        ],
        deadline=deadline,
    )
    if _cli_hash(path) != observer["sha256"]:
        raise PreflightError("engine_fit_lms_cli_changed")
    remaining(deadline)
    return result


def _model_row(value, observer, model_path):
    if not isinstance(value, list) or len(value) != 1 or not isinstance(value[0], dict):
        raise PreflightError("engine_fit_lms_loaded_instance_ambiguous")
    row = value[0]
    if (
        row.get("type") != "llm"
        or row.get("format") != "gguf"
        or row.get("identifier") != observer["instance_id"]
        or "deviceIdentifier" not in row
        or row["deviceIdentifier"] is not None
    ):
        raise PreflightError("engine_fit_lms_local_instance_unverified")
    relative = row.get("path")
    if not isinstance(relative, str) or not relative or "\\" in relative or "\0" in relative:
        raise PreflightError("engine_fit_lms_model_path_unverified")
    path = PurePosixPath(relative)
    if path.is_absolute() or any(part in (".", "..", "") for part in relative.split("/")):
        raise PreflightError("engine_fit_lms_model_path_unverified")
    root = Path(observer["models_root"])
    local = (root / relative).resolve(strict=True)
    # File symlinks can name the frozen asset, but cannot escape the declared model root.
    if not local.is_relative_to(root) or not local.is_file() or not local.samefile(model_path):
        raise PreflightError("engine_fit_lms_model_path_unverified")
    if (
        not isinstance(row.get("status"), str)
        or row["status"] not in _STATES
        or type(row.get("queued")) is not int
        or row["queued"] < 0
    ):
        raise PreflightError("engine_fit_lms_processing_state_unavailable")
    return {"status": row["status"], "queued": row["queued"]}


def observe(observer, address, port, model_path, *, deadline=None):
    """Validate local asset and disabled LM Link before returning only needed state."""
    try:
        deadline = deadline if deadline is not None else time.monotonic() + TOTAL_TIMEOUT
        link = _query(observer, ["link", "status"], address, port, deadline)
        if (
            not isinstance(link, dict)
            or not isinstance(link.get("issues"), list)
            or not all(isinstance(issue, str) for issue in link["issues"])
            or "deviceDisabled" not in link["issues"]
        ):
            raise PreflightError("engine_fit_lms_link_not_disabled")
        result = _model_row(_query(observer, ["ps"], address, port, deadline), observer, model_path)
        remaining(deadline)
        return result
    except (OSError, ValueError, RuntimeError) as exc:
        if isinstance(exc, PreflightError):
            raise
        raise PreflightError("engine_fit_lms_observer_unavailable") from exc


def bind_observer(lms_path, models_root, served_model, model_path, address, port, *, deadline=None):
    """Create a stable CLI/asset binding, keeping private CLI output out of evidence."""
    if (
        lms_path is None
        or models_root is None
        or not isinstance(served_model, str)
        or not served_model
    ):
        raise PreflightError("engine_fit_lms_binding_options_required")
    try:
        deadline = deadline if deadline is not None else time.monotonic() + TOTAL_TIMEOUT
        remaining(deadline)
        path = Path(lms_path).resolve(strict=True)
        root = Path(models_root).resolve(strict=True)
        model = Path(model_path).resolve(strict=True)
        if not root.is_dir() or not model.is_file():
            raise PreflightError("engine_fit_lms_model_path_unverified")
        observer = {
            "kind": "lms",
            "path": str(path),
            "sha256": _cli_hash(path),
            "models_root": str(root),
            "instance_id": served_model,
        }
        observe(observer, address, port, model, deadline=deadline)
        return observer
    except (OSError, ValueError, RuntimeError) as exc:
        if isinstance(exc, PreflightError):
            raise
        raise PreflightError("engine_fit_lms_observer_unavailable") from exc


class LMStudioObserver:
    def __init__(self, binding):
        self.binding = binding

    async def _state(self):
        binding = self.binding
        state = await asyncio.to_thread(
            observe,
            binding["observer"],
            binding["address"],
            binding["port"],
            binding["model_binding"]["path"],
        )
        try:
            current = Path(binding["model_binding"]["path"]).stat()
        except OSError as exc:
            raise PreflightError("engine_fit_lms_observer_unavailable") from exc
        if (current.st_dev, current.st_ino) != (
            binding["model_binding"]["device"],
            binding["model_binding"]["inode"],
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        return state

    async def inspect(self):
        await self._state()
        return {
            "engine": "lmstudio",
            "version": None,
            "served_model": self.binding["observer"]["instance_id"],
            "version_source": "not_exposed",
            "version_missing_reason": "lmstudio_service_version_not_exposed",
        }

    async def idle(self):
        state = await self._state()
        labels = {"instance": self.binding["observer"]["instance_id"]}
        active = int(state["status"] != "idle")
        return {
            "idle": active == 0 and state["queued"] == 0,
            "source": "lms:ps",
            "values": [
                {"metric": "lmstudio:queued", "labels": labels, "value": state["queued"]},
                {"metric": "lmstudio:active", "labels": labels, "value": active},
            ],
        }
