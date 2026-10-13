"""Operator-bound length preparation; management calls only, no generation."""

import asyncio
import os
from dataclasses import asdict

from inferyard.application.types import CommandResult
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    read_json,
    sha256_file,
)
from inferyard.platforms.identity import PreflightError
from inferyard.provenance import tool_source_hash
from inferyard.registry import adapter_factory
from inferyard.reporting.report_common import _new_output
from inferyard.runtime.batch_state import bind_service
from inferyard.runtime.length_builder import fit_length, validate_spec
from inferyard.runtime.runner import Dependencies, transport_config
from inferyard.runtime.template_tokens import count_template


async def prepare(request, dependencies=None):
    spec = read_json(request.length_spec)
    validate_spec(spec)
    loaded, binding = bind_service(request.config, request.endpoint_url, request.server_pid)
    config = loaded.config.to_dict()
    if (
        spec["target_tokens"] + spec["tolerance_tokens"] + config["generation"]["max_tokens"]
        > config["conditions"]["context_size"]
    ):
        raise PreflightError("declared_length_context_budget_exceeded")
    deps = dependencies or Dependencies(adapter=adapter_factory(config["engine"]["adapter"]))
    secret_name = config["endpoint"].get("api_key_env")
    secret = os.environ.get(secret_name) if secret_name else None
    if secret_name and not secret:
        raise PreflightError("credential_reference_unavailable")
    with deps.lock() as lock:
        if lock.state and lock.state.get("dirty"):
            raise PreflightError("length_preparation_requires_clean_service")
        identity, files = deps.preflight(config)
        adapter = deps.adapter(identity["origin"], secret=secret)
        try:
            guard_config = transport_config(config, adapter)
            deps.guard(guard_config, files)
            if adapter.redactor.clean(spec) != spec:
                raise EvidenceError("length_spec_contains_redacted_input")
            props = await adapter.verify_properties(config)
            if not await adapter.wait_idle(config["execution"]["idle_wait_seconds"]):
                raise PreflightError("length_preparation_service_not_idle")
            _new_output(request.out, [request.length_spec, request.config.source])
            for name, value in (
                ("spec.json", spec),
                ("config.frozen.json", config),
                ("service-binding.json", binding),
                ("service.props.json", props),
                ("identity.json", {**identity, "files": [asdict(f) for f in files]}),
            ):
                atomic_bytes(request.out / name, json_bytes(value))
            trace = []

            async def count(prompt):
                deps.guard(guard_config, files)
                measurement = await count_template(adapter, config, prompt)
                deps.guard(guard_config, files)
                return measurement

            def observe(record):
                trace.append(record)
                atomic_bytes(request.out / "probes.json", json_bytes(trace), overwrite=True)

            try:
                async with asyncio.timeout(spec["max_wall_seconds"]):
                    result = await fit_length(spec, count, observe=observe)
            except TimeoutError:
                result = {
                    "status": "wall_budget_exhausted",
                    "prompt": None,
                    "selected": None,
                    "probes": trace,
                }
            prompt = result.pop("prompt")
            if prompt is not None:
                atomic_bytes(request.out / "prompt.txt", prompt.encode())
            result.update(
                tool_source_sha256=tool_source_hash(),
                scope="model_specific_performance_text_not_shared_quality_corpus",
                limitations=[
                    "preparation_not_benchmark",
                    "requires_frozen_plan_and_runtime_recheck",
                ],
            )
            atomic_bytes(request.out / "result.json", json_bytes(result))
            manifest = {
                "files": {p.name: sha256_file(p) for p in request.out.iterdir() if p.is_file()}
            }
            atomic_bytes(request.out / "preparation-manifest.json", json_bytes(manifest))
            return 0 if result["status"] == "matched" else 3, CommandResult(
                request.command,
                result["status"],
                "complete" if prompt is not None else "incomplete",
                evidence_dir=str(request.out),
                limitations=tuple(result["limitations"]),
                details=result,
            )
        finally:
            await adapter.close()


def execute(request):
    return asyncio.run(prepare(request))
