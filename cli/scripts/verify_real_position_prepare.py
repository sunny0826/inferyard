"""Measure frozen position prompts through management endpoints without generation."""

import argparse
import asyncio
import hashlib
import socket
import subprocess
import time
import urllib.error
import urllib.request
from dataclasses import asdict
from pathlib import Path

from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, sha256_file
from inferyard.provenance import tool_source_hash
from inferyard.runtime.batch_state import bind_service
from inferyard.runtime.runner import Dependencies
from inferyard.runtime.template_tokens import count_template


def save(root, name, value):
    atomic_bytes(root / name, json_bytes(value))


async def close_owned_service(adapter, process, out):
    """Always stop the owned server, including when adapter cleanup fails."""
    try:
        if adapter is not None:
            await adapter.close()
    finally:
        try:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        finally:
            save(
                out,
                "lifecycle.json",
                {
                    "owned_pid": process.pid,
                    "stopped": process.poll() is not None,
                    "exit_code": process.returncode,
                },
            )
            save(
                out,
                "manifest.json",
                {"files": {p.name: sha256_file(p) for p in out.iterdir() if p.is_file()}},
            )


async def prepare(plan_path, out):
    plan, loaded = read_frozen_plan(plan_path)
    workloads = plan["experiment"]["workloads"]
    if not workloads or any(w["purpose"] != "position" for w in workloads):
        raise EvidenceError("position_preparation_requires_position_plan")
    configs = [loaded[w["workload_id"]].config.to_dict() for w in workloads]
    if any(c != configs[0] for c in configs):
        raise EvidenceError("position_preparation_requires_identical_configs")
    source = {str(p): sha256_file(p) for p in plan_path.parent.rglob("*") if p.is_file()}
    config = configs[0]
    if config["endpoint"].get("api_key_env"):
        raise EvidenceError("owned_local_preparation_requires_no_credentials")
    out.mkdir(parents=True, exist_ok=False)
    save(
        out,
        "protocol.json",
        {
            "plan_path": str(plan_path),
            "plan_sha256": plan["plan_sha256"],
            "max_wall_seconds": 300,
            "generation_requests": 0,
            "scope": "management_token_counts_not_generation_or_formal_admission",
            "tool_source_sha256": tool_source_hash(),
        },
    )
    deps = Dependencies()
    with deps.lock() as lock:
        if lock.state and lock.state.get("dirty"):
            raise EvidenceError("position_preparation_requires_clean_service")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        startup = list(config["engine"]["startup_args"])
        startup[startup.index("--port") + 1] = str(port)
        command = [config["engine"]["binary_path"], *startup]
        save(out, "service-command.json", command)
        with (out / "server.log").open("wb") as log:
            process = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
            adapter = None
            try:
                bound, binding = bind_service(
                    loaded[workloads[0]["workload_id"]], f"http://127.0.0.1:{port}", process.pid
                )
                config = bound.config.to_dict()
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                deadline = time.monotonic() + 120
                while True:
                    if process.poll() is not None:
                        raise EvidenceError("owned_service_exited")
                    try:
                        with opener.open(config["endpoint"]["url"] + "/health", timeout=2):
                            break
                    except urllib.error.URLError, TimeoutError:
                        if time.monotonic() >= deadline:
                            raise
                        await asyncio.sleep(0.5)
                identity, files = deps.preflight(config)
                adapter = deps.adapter(identity["origin"])
                props = await adapter.verify_properties(config)
                if not await adapter.wait_idle(config["execution"]["idle_wait_seconds"]):
                    raise EvidenceError("position_preparation_service_not_idle")
                for name, value in (
                    ("config.frozen.json", config),
                    ("service-binding.json", binding),
                    ("service.props.json", props),
                    ("identity.json", {**identity, "files": [asdict(f) for f in files]}),
                ):
                    save(out, name, value)
                rows = []
                for workload in workloads:
                    cases = {
                        c["case_id"]: c
                        for c in loaded[workload["workload_id"]].bundle.to_dict()["cases"]
                    }
                    for case_id in workload["protocol"]["case_ids"]:
                        prompt = cases[case_id]["prompt"]
                        if adapter.redactor.clean(prompt) != prompt:
                            raise EvidenceError("position_prompt_contains_redacted_input")
                        deps.guard(config, files)
                        measurement = await count_template(adapter, config, prompt)
                        deps.guard(config, files)
                        rows.append(
                            {
                                "workload_id": workload["workload_id"],
                                "case_id": case_id,
                                "prompt_sha256": hashlib.sha256(prompt.encode()).hexdigest(),
                                **measurement,
                                "context_size": config["conditions"]["context_size"],
                                "within_declared_context_budget": measurement["input_tokens"]
                                + measurement["output_budget"]
                                <= config["conditions"]["context_size"],
                            }
                        )
                        atomic_bytes(out / "counts.json", json_bytes(rows), overwrite=True)
                unchanged = all(sha256_file(Path(p)) == digest for p, digest in source.items())
                if not unchanged:
                    raise EvidenceError("position_source_changed")
                save(out, "source-hashes.json", source)
                save(
                    out,
                    "verification.json",
                    {
                        "passed": True,
                        "measured_variants": len(rows),
                        "all_within_declared_context_budget": all(
                            r["within_declared_context_budget"] for r in rows
                        ),
                        "formal_requests_executed": 0,
                        "source_unchanged": unchanged,
                        "limitations": [
                            "runtime_recount_required",
                            "not_server_generation_admission",
                            "not_quality_or_performance_acceptance",
                            "human_corpus_review_pending",
                        ],
                    },
                )
            finally:
                await close_owned_service(adapter, process, out)


async def bounded(plan, out):
    async with asyncio.timeout(300):
        await prepare(plan, out)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    asyncio.run(bounded(args.plan.resolve(), args.out.resolve()))


if __name__ == "__main__":
    main()
