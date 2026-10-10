"""Serial runner for an externally managed service, with durable request boundaries."""

from __future__ import annotations

import asyncio
import os
import time
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit

from inferyard.adapters.prism import PrismAdapter
from inferyard.analysis.scoring import ScoringContext, score_case
from inferyard.application.types import CommandResult
from inferyard.config.bundle import require_review
from inferyard.config.loader import LoadedConfig
from inferyard.config.single_plan import compile_single_plan
from inferyard.config.startup_arguments import replace_arguments
from inferyard.contracts.validation import Document
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import (
    Redactor,
    sha256_file,
    verify_manifest,
)
from inferyard.implementation_identity import IdentityContext
from inferyard.platforms.identity import (
    PreflightError,
    environment_snapshot,
    process_start_ticks,
    service_origin,
    static_preflight,
    verify_process,
)
from inferyard.platforms.telemetry import Sampler
from inferyard.provenance import tool_source_hash
from inferyard.registry import adapter_collector_id, adapter_factory, collector_factory
from inferyard.runtime.lock import HostLock


def _guard(config, files):
    if not all(item.unchanged() for item in files):
        raise PreflightError("identity_file_changed")
    _, address, port = service_origin(config["endpoint"]["url"])
    verify_process(
        config,
        files[0],
        files[1],
        address,
        port,
        bound_files=files,
    )


def transport_config(config, adapter):
    """Check the listener actually pinned by the adapter without changing frozen inputs."""
    origin = getattr(adapter, "origin", None)
    if not isinstance(origin, str):
        return config
    return {**config, "endpoint": {**config["endpoint"], "url": origin}}


@dataclass
class Dependencies:
    preflight: object = static_preflight
    adapter: object = PrismAdapter
    lock: object = HostLock
    sampler: object = Sampler
    guard: object = _guard
    scorer: object = score_case
    environment: object = environment_snapshot


def load_rerun(request, scoring_context=None, identity_context=None) -> tuple[LoadedConfig, dict]:
    root = request.from_run
    limitations = verify_manifest(root)
    if any("manifest_missing" in reason for reason in limitations):
        raise PreflightError("rerun_requires_sealed_source")
    metadata = {}
    parent = read_trial(root, metadata=metadata)
    parent["service_drain"] = metadata.get("service_drain")
    config, bundle, meta = (deepcopy(parent[k]) for k in ("config", "bundle", "run"))
    if meta["execution_mode"] != "single" or meta["kind"] != "run":
        raise PreflightError("rerun_requires_single_run_source")
    new_start = process_start_ticks(request.server_pid)
    old_url = urlsplit(config["endpoint"]["url"])
    new_url = urlsplit(request.endpoint_url)
    config["engine"]["startup_args"] = replace_arguments(
        config["engine"]["startup_args"],
        {
            "--host": new_url.hostname,
            "--port": str(new_url.port or (443 if new_url.scheme == "https" else 80)),
        },
        expected_values={"--host": old_url.hostname},
    )
    config["endpoint"].update(
        url=request.endpoint_url, server_pid=request.server_pid, process_start_ticks=new_start
    )
    from inferyard.runtime.service_reuse import require_transition, same_process

    if same_process(parent["config"]["endpoint"], config["endpoint"]):
        raise PreflightError("rerun_requires_new_service")
    require_transition(parent, config, process_start_ticks, reason="rerun_requires_new_service")
    if getattr(request, "output_root", None):
        config["output"]["root"] = str(request.output_root.resolve())
    if getattr(request, "api_key_env", None):
        config["endpoint"]["api_key_env"] = request.api_key_env
    source = root / "config.input.toml"
    if not source.exists():
        source = root / "config.frozen.json"
    return LoadedConfig(
        source, Document.parse("config", config), Document.parse("bundle", bundle)
    ), parent


async def execute_async(request, dependencies: Dependencies | None = None):
    scoring_context = ScoringContext()
    identity_context = IdentityContext(source_hash=tool_source_hash)
    loaded, parent = (
        load_rerun(request, scoring_context, identity_context)
        if request.from_run
        else (request.config, None)
    )
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    deps = dependencies or Dependencies(
        adapter=adapter_factory(config["engine"]["adapter"]),
        sampler=collector_factory(adapter_collector_id(config["engine"]["adapter"])),
    )
    key_name = config["endpoint"].get("api_key_env")
    secret = os.environ.get(key_name) if key_name else None
    if key_name and not secret:
        raise PreflightError("credential_reference_unavailable")
    redactor = Redactor([secret] if secret else [])
    plan = compile_single_plan(loaded.config, loaded.bundle)
    started_at = time.monotonic()
    allowed_seconds = plan["experiment"]["budget"]["max_wall_seconds"]
    store = TrialJournal(
        Path(config["output"]["root"]),
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle,
        redactor=redactor,
        rerun_parent=parent,
        diagnostic=request.diagnostic,
        kind=request.command,
        execution_mode="single",
        scoring_context=scoring_context,
        source_identity=identity_context.source,
        implementation_identity=identity_context.value,
    )
    source_name = "config.input.toml" if loaded.source.suffix == ".toml" else "config.input.json"
    store.text_snapshot(source_name, loaded.source.read_text())
    if parent:
        store.snapshot(
            "parent.json",
            {
                "run_id": parent["run"]["run_id"],
                "path": str(request.from_run),
                "manifest_sha256": sha256_file(request.from_run / "manifest.json"),
                "events_sha256": parent["events_sha256"],
            },
        )
    from inferyard.runtime.trial_profile import SingleProfile
    from inferyard.runtime.trial_runner import run_trial

    code, outcome, _ = await run_trial(
        plan,
        plan["trials"][0]["trial_id"],
        loaded,
        Path(config["output"]["root"]),
        parent=parent,
        diagnostic=request.diagnostic,
        recovery_confirm=request.recovery_confirm,
        recovery_note=request.recovery_note,
        dependencies=deps,
        profile=SingleProfile(
            store=store,
            kind=request.command,
            secret=secret,
            started_at=started_at,
            allowed_seconds=allowed_seconds,
            review=require_review,
            sleep=asyncio.sleep,
            monotonic=time.monotonic,
        ),
    )
    summary, reason = outcome["summary"], outcome["reason"]
    return code, CommandResult(
        request.command,
        "finished"
        if code == 0
        else "interrupted"
        if code == 130
        else "blocked"
        if code == 2
        else "error",
        summary["completeness"] if summary else "incomplete",
        store.run_id,
        str(store.path),
        tuple(
            ([reason] if reason != "plan_finished" else [])
            + (summary["limitations"] if summary else [])
        ),
        details={
            **outcome["observation"],
            "evidence_only": True,
            "next_report_command": [
                "inferyard",
                "report",
                "--runs",
                str(store.path),
                "--out",
                str(store.path.parent / (store.run_id + "-report")),
            ],
        },
    )


def execute(request):
    return asyncio.run(execute_async(request))
