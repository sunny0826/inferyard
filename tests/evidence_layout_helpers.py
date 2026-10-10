"""Test-only packing of newly generated synthetic originals; no production conversion API."""

import base64
import hashlib
import re

from inferyard.evidence.layout import INPUT_FILES, OBSERVATION_FILES
from inferyard.evidence.storage import json_bytes, read_json, read_jsonl, sha256_file


def digest(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def seal_layout(root, run):
    files = {
        path.name: {"sha256": sha256_file(path), "bytes": path.stat().st_size, "derived": False}
        for path in root.iterdir()
        if path.is_file() and path.name != "manifest.json"
    }
    manifest = {
        "schema_version": 3,
        "evidence_layout_version": 2,
        "run_id": run["run_id"],
        "sealed": True,
        "files": files,
        **{
            key: run[key]
            for key in ("kind", "execution_mode", "origin", "experiment_id", "trial_id")
        },
    }
    (root / "manifest.json").write_bytes(json_bytes(manifest))


def pack_layout2(source, destination):
    """Only fixture callers may pack their fresh temporary run into a new directory."""
    from inferyard.evidence.request_snapshots import snapshot_name_for_event

    events, _ = read_jsonl(source / "events.jsonl")
    for event in events:
        if (
            event["event_type"] == "request_started"
            and not (source / snapshot_name_for_event(source, event)).is_file()
        ):
            raise ValueError("synthetic layout packing requires original sent request bodies")
    destination.mkdir()
    documents = {kind: read_json(source / name) for kind, name in INPUT_FILES.items()}
    run = documents["run"]
    original_sources = [
        source / name
        for name in ("config.input.toml", "config.input.json")
        if (source / name).exists()
    ]
    configuration_source = None
    if original_sources:
        original = original_sources[0]
        raw = original.read_bytes()
        configuration_source = {
            "media_type": "application/toml" if original.suffix == ".toml" else "application/json",
            "encoding": "base64",
            "content": base64.b64encode(raw).decode("ascii"),
            "sha256": hashlib.sha256(raw).hexdigest(),
            "bytes": len(raw),
        }
    inputs = {
        "schema_version": 3,
        "evidence_layout_version": 2,
        "run_id": run["run_id"],
        "documents": {
            kind: {"payload": value, "sha256": digest(value)} for kind, value in documents.items()
        },
        "configuration_source": configuration_source,
        "configuration_source_missing_reason": None if configuration_source else "not_persisted",
    }
    (destination / "inputs.json").write_bytes(json_bytes(inputs))
    rows = []
    for (kind, stage), name in OBSERVATION_FILES.items():
        if not (source / name).exists():
            continue
        stage = stage or (
            "post_probe"
            if kind == "identity"
            else "pre_idle"
            if documents["config"]["engine"]["adapter"] in ("kvmem", "ninfer")
            else "post_idle"
        )
        payload = read_json(source / name)
        rows.append(
            {
                "schema_version": 3,
                "evidence_layout_version": 2,
                "run_id": run["run_id"],
                "trial_id": run["trial_id"],
                "seq": len(rows) + 1,
                "kind": kind,
                "stage": stage,
                "key": None,
                "clock_id": "fixture-clock",
                "monotonic_ns": len(rows),
                "utc": "2026-10-10T00:00:00Z",
                "payload": payload,
                "payload_sha256": digest(payload),
            }
        )
    from inferyard.evidence.native_receipts import clean_receipt_name

    for path in sorted(source.glob("client-http-clean-*.json")):
        payload = read_json(path)
        key = payload["request_id"]
        if path.name != clean_receipt_name(key):
            raise ValueError("fixture receipt filename does not bind request identity")
        rows.append(
            {
                "schema_version": 3,
                "evidence_layout_version": 2,
                "run_id": run["run_id"],
                "trial_id": run["trial_id"],
                "seq": len(rows) + 1,
                "kind": "native_clean_receipt",
                "stage": "client_complete",
                "key": key,
                "clock_id": "fixture-clock",
                "monotonic_ns": len(rows),
                "utc": "2026-10-10T00:00:00Z",
                "payload": payload,
                "payload_sha256": digest(payload),
            }
        )
    (destination / "observations.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    events, _ = read_jsonl(source / "events.jsonl")
    started = {
        event["request_id"]: event for event in events if event["event_type"] == "request_started"
    }
    prepared = []
    for path in sorted(source.glob("*.request.json")):
        match = re.fullmatch(r"(.+)__([0-9]+)__(probe|warmup|formal)\.request\.json", path.name)
        if match is None:
            raise ValueError("fixture is not a current original run")
        ordinal, phase = int(match[2]), match[3]
        key = next(
            (
                key
                for key, event in started.items()
                if event["phase"] == phase and key.endswith((f"-{ordinal}", f":{ordinal}"))
            ),
            f"{run['run_id']}-{phase}-{ordinal}",
        )
        body = read_json(path)
        prepared.append(
            {
                "schema_version": 3,
                "evidence_layout_version": 2,
                "run_id": run["run_id"],
                "trial_id": run["trial_id"],
                "request_id": key,
                "ordinal": ordinal,
                "phase": phase,
                "body": body,
                "body_sha256": digest(body),
            }
        )
    (destination / "request-bodies.jsonl").write_bytes(b"".join(map(json_bytes, prepared)))
    for name in ("events.jsonl", "memory.jsonl", "environment.jsonl", "schedule.jsonl"):
        (destination / name).write_bytes((source / name).read_bytes())
    if (source / "manifest.json").exists():
        seal_layout(destination, run)
    return destination


def build_current_original(root, config_path=None, *, diagnostic=False):
    """Create a synthetic run with sent bodies saved before their started events."""
    from pathlib import Path

    from inferyard.adapters.requests import request_body
    from inferyard.analysis.scoring import score_case
    from inferyard.config.loader import load_config
    from inferyard.config.single_plan import compile_single_plan
    from inferyard.evidence.journal import TrialJournal
    from inferyard.evidence.request_snapshots import messages_sha256, snapshot_filename

    loaded = load_config(config_path or Path(__file__).parent / "fixtures/config/valid.toml")
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    config["output"]["root"] = str(root)
    config["execution"]["warmup_count"] = 0
    config["conditions"].update(cache_policy="disabled", model_loaded=True)
    config["generation"]["seed_support"] = "supported"
    plan = compile_single_plan(config, bundle)
    store = TrialJournal(
        root,
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle,
        diagnostic=diagnostic,
        execution_mode="single",
    )
    env = {
        "platform": "Linux",
        "architecture": "x86_64",
        "kernel": "fixture-kernel",
        "os_release": {"ID": "fixture", "VERSION_ID": "1"},
        "cpu_flags": ["fixture_flag"],
        "scaling_driver": "fixture_driver",
        "cpu_model": "fixture-cpu",
        "logical_cpus": 12,
        "memory_total_bytes": 40 * 1024**3,
        "ac_online": True,
        "profile": "balanced",
        "governor": "powersave",
        "epp": "balance_performance",
        "swap_pages": {"pswpin": 0, "pswpout": 0},
    }
    params = {
        key: {
            "requested": config["generation"][key],
            "effective": config["generation"][key],
            "verification": "verified",
            "source": "synthetic_fixture",
        }
        for key in (
            "seed",
            "temperature",
            "top_k",
            "top_p",
            "min_p",
            "presence_penalty",
            "repeat_penalty",
            "max_tokens",
        )
    }
    identity = {
        "verification": "verified",
        "source": "synthetic_fixture",
        "runtime_library_hashes": {"fixture.so": "a" * 64},
        "effective_parameters": {"parameters": params},
    }
    store.text_snapshot("config.input.toml", loaded.source.read_text())
    store.snapshot("environment.start.json", env)
    requests = [
        ("probe", None, config["execution"]["probe_prompt"], False),
        ("probe", None, config["execution"]["probe_prompt"], True),
        *(("formal", index, case["prompt"], True) for index, case in enumerate(bundle["cases"])),
    ]
    try:
        for ordinal, (phase, index, prompt, stream) in enumerate(requests, 1):
            body = request_body(config, prompt, stream=stream)
            key = f"{store.run_id}-{phase}-{ordinal}"
            store.snapshot(snapshot_filename(store.run_id, phase, ordinal), body)
            start, end = ordinal * 10**9, (ordinal + 1) * 10**9 - 1
            case = bundle["cases"][index] if index is not None else None
            answer = case["reference_answer"] if case else "synthetic-probe"
            store.event(
                "request_started",
                phase,
                key,
                {
                    "case_id": case["case_id"] if case else None,
                    "plan_index": index,
                    "attempt": 1,
                    "body": {
                        "model": body["model"],
                        "messages_sha256": messages_sha256(body["messages"]),
                        "stream": stream,
                        "generation": config["generation"],
                    },
                },
                monotonic_ns=start,
            )
            store.event("content", phase, key, {"text": answer}, monotonic_ns=start + 1)
            store.event(
                "usage",
                phase,
                key,
                {
                    "completion_tokens": 2,
                    "prompt_tokens": 1,
                    "source": "endpoint.usage",
                    "scope": "completion_tokens",
                },
                monotonic_ns=start + 2,
            )
            store.event("finish", phase, key, {"raw_finish_reason": "stop"}, monotonic_ns=end - 1)
            store.event("protocol_end", phase, key, {}, monotonic_ns=end)
            store.event(
                "request_finished",
                phase,
                key,
                {
                    "execution_state": "completed",
                    "raw_finish_reason": "stop",
                    "budget_exhausted": False,
                    "error_category": None,
                    "protocol_complete": True,
                    "t_send_ns": start,
                    "t_first_content_ns": start + 1,
                    "t_first_answer_ns": start + 1,
                    "t_terminal_ns": end,
                    "content": answer,
                    "reasoning": "",
                    "completion_tokens": 2,
                    "token_source": "endpoint.usage",
                    "token_scope": "completion_tokens",
                },
                monotonic_ns=end,
            )
            if phase == "probe":
                store.snapshot(
                    "effective-stream.json" if stream else "effective-ordinary.json",
                    {"parameters": params},
                )
                if stream:
                    store.snapshot("identity.json", identity)
            if case:
                store.event(
                    "score",
                    "scoring",
                    key,
                    score_case(case, answer, bundle["answer_policy"]),
                    monotonic_ns=end + 1,
                )
                for metric, value in (
                    ("system_mem_available", 8 * 1024**3),
                    ("service_rss", 7 * 1024**3),
                ):
                    store.sample(
                        {
                            "phase": phase,
                            "request_id": key,
                            "metric_name": metric,
                            "value": value,
                            "unit": "bytes",
                            "source": "synthetic_fixture",
                            "read_started_ns": start + 10,
                            "read_finished_ns": start + 11,
                            "server_pid": config["endpoint"]["server_pid"]
                            if metric == "service_rss"
                            else None,
                            "process_start_ticks": config["endpoint"]["process_start_ticks"]
                            if metric == "service_rss"
                            else None,
                            "missing_reason": None,
                        }
                    )
        store.snapshot("environment.end.json", env)
        store.event(
            "run_stopped",
            "finalizing",
            None,
            {"reason": "plan_finished"},
            monotonic_ns=(len(requests) + 1) * 10**9,
        )
        store.seal()
    finally:
        store.close()
    return store.path
