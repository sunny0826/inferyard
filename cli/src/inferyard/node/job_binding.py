"""Loopback, frozen-plan and local-engine rerun binding helpers for remote jobs."""

from __future__ import annotations

import json
from pathlib import Path
from urllib.parse import urlsplit


def validate_loopback(url: str) -> None:
    parsed = urlsplit(url)
    if parsed.scheme != "http" or parsed.hostname != "127.0.0.1":
        raise ValueError("remote jobs require a loopback endpoint")


def validate_frozen_plan(plan_path: Path, expected: dict | None) -> None:
    from inferyard.config.plan_inputs import read_frozen_plan

    _, workloads = read_frozen_plan(plan_path)
    for loaded in workloads.values():
        endpoint = loaded.config.to_dict()["endpoint"]
        url = endpoint.get("url", "")
        validate_loopback(url)
        if expected is not None:
            if url != expected.get("url") or endpoint.get("server_pid") != expected.get(
                "server_pid"
            ):
                raise ValueError("frozen plan endpoint does not match expected service binding")


def prepare_rerun_binding(
    job_dir: Path, from_run: Path, endpoint: dict, write_json
) -> Path | None:
    inputs_path = from_run / "inputs.json"
    if not inputs_path.is_file():
        return None
    from inferyard.contracts.engine_profiles import is_local
    from inferyard.platforms.identity import process_start_ticks
    from inferyard.platforms.local_engines.service import bind_service
    from inferyard.platforms.startup_arguments import replace_arguments

    payload = json.loads(inputs_path.read_text(encoding="utf-8"))
    config = payload.get("config")
    if not isinstance(config, dict) or not is_local(config):
        return None
    cfg = json.loads(json.dumps(config))
    new_url = urlsplit(endpoint["url"])
    old_url = urlsplit(cfg["endpoint"]["url"])
    cfg["engine"]["startup_args"] = replace_arguments(
        cfg["engine"]["startup_args"],
        {
            "--host": new_url.hostname,
            "--port": str(new_url.port or (443 if new_url.scheme == "https" else 80)),
        },
        expected_values={"--host": old_url.hostname},
    )
    cfg["endpoint"].update(
        url=endpoint["url"],
        server_pid=endpoint["server_pid"],
        process_start_ticks=endpoint.get("process_start_ticks")
        or process_start_ticks(endpoint["server_pid"]),
    )
    binding = bind_service(cfg)
    binding_path = job_dir / "service-binding.json"
    write_json(binding_path, binding)
    return binding_path
