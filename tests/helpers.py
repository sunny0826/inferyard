"""Hand-specified synthetic evidence; never a measured model result."""

import queue
import sys
import threading
from copy import deepcopy
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.scoring import score_case
from inferyard.config.loader import load_config
from inferyard.config.plan_math import plan_hash
from inferyard.config.single_plan import compile_single_plan
from inferyard.evidence.journal import TrialJournal


def python_worker(script, *arguments):
    if sys.platform == "win32":
        # The venv python.exe is a redirector that spawns another PID. Use the
        # base interpreter so kill() reaches the actual worker holding the lock.
        packages = str(Path(sys.prefix) / "Lib/site-packages")
        script = f"import site; site.addsitedir({packages!r});\n" + script
        return [sys._base_executable, "-c", script, *arguments]
    return [sys.executable, "-c", script, *arguments]


def symlink_or_skip(link, target):
    import pytest

    try:
        link.symlink_to(target)
    except OSError as exc:
        if sys.platform == "win32" and exc.winerror == 1314:
            pytest.skip("Symlink creation requires Windows Developer Mode or privilege")
        raise


def readline_timeout(process, seconds=8):
    """Wait for one worker line; Windows select() only accepts sockets, not pipes."""
    result = queue.Queue()

    def read():
        result.put(process.stdout.readline())

    threading.Thread(target=read, daemon=True).start()
    try:
        return result.get(timeout=seconds).strip()
    except queue.Empty as exc:
        raise AssertionError("worker did not reach the expected boundary") from exc


def fixture_run(
    root,
    states=None,
    passes=None,
    durations=None,
    model="synthetic-A",
    budget_exhausted=(),
    *,
    comparison_mode="side-by-side",
):
    """Write a real contract/journal fixture, without contacting a model service."""
    states = ["completed", "completed", "failed"] if states is None else list(states)
    passes = {0, 1} if passes is None else set(passes)
    durations = list(range(1, len(states) + 1)) if durations is None else durations
    if len(durations) != len(states):
        raise ValueError("each synthetic request needs a duration")
    for index, state in enumerate(states):
        if state not in ("completed", "failed", "cancelled", "invalid", "not_executed"):
            raise ValueError("unknown synthetic execution state")
        if state in ("invalid", "not_executed") and any(
            later != "not_executed" for later in states[index + 1 :]
        ):
            raise ValueError("unresolved or unexecuted request must end the synthetic sequence")
    original = load_config(Path(__file__).parent / "fixtures/config/valid.toml")
    config = original.config.to_dict()
    config["schema_version"] = SCHEMA_VERSION
    config["model"]["display_name"] = model
    config["generation"]["seed_support"] = "supported"
    config["conditions"]["cache_policy"] = "disabled"
    config["conditions"]["model_loaded"] = True
    config["output"]["root"] = str(root)
    bundle = original.bundle.to_dict()
    bundle["schema_version"] = SCHEMA_VERSION
    seed = deepcopy(bundle["cases"][0])
    bundle["cases"] = [dict(deepcopy(seed), case_id=f"case-{i}") for i in range(len(states))]
    bundle["cases"][0]["prompt"] = "<script>alert(1)</script> {{7*7}} "
    plan = compile_single_plan(config, bundle)
    if comparison_mode != "side-by-side":
        plan["experiment"]["comparison"] = {"mode": comparison_mode, "factor": None}
        plan["plan_sha256"] = plan_hash(plan)
    store = TrialJournal(
        root, plan, plan["trials"][0]["trial_id"], config, bundle, execution_mode="single"
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
    store.snapshot("environment.start.json", env)
    store.snapshot("environment.end.json", env)
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
    store.snapshot(
        "identity.json",
        {
            "verification": "verified",
            "source": "synthetic_fixture",
            "runtime_library_hashes": {"fixture.so": "a" * 64},
            "effective_parameters": {"parameters": params},
        },
    )
    cursor = 10**9
    for i, state in enumerate(states):
        if state == "not_executed":
            continue
        key = f"req-{i}"
        start = cursor
        end = start + int(durations[i] * 10**9)
        cursor = end + 10**9
        case = bundle["cases"][i]
        store.event(
            "request_started",
            "formal",
            key,
            {
                "case_id": case["case_id"],
                "plan_index": i,
                "attempt": 1,
                "body": {
                    "model": model,
                    "messages": [{"role": "user", "content": case["prompt"]}],
                    "stream": True,
                    "generation": config["generation"],
                },
            },
            monotonic_ns=start - 1,
        )
        if state == "invalid":
            continue
        answer = (
            case["reference_answer"]
            if i in passes
            else "<img src=x onerror=alert(1)> <script>alert(2)</script> {{7*7}}"
        )
        store.event("content", "formal", key, {"text": answer}, monotonic_ns=start + 1)
        store.event(
            "usage",
            "formal",
            key,
            {
                "completion_tokens": 2,
                "prompt_tokens": 1,
                "source": "endpoint.usage",
                "scope": "completion_tokens",
            },
            monotonic_ns=start + 2,
        )
        finish = ("length" if i in budget_exhausted else "stop") if state == "completed" else None
        if state == "completed":
            store.event(
                "finish", "formal", key, {"raw_finish_reason": finish}, monotonic_ns=end - 1
            )
            store.event("protocol_end", "formal", key, {}, monotonic_ns=end)
        store.event(
            "request_finished",
            "formal",
            key,
            {
                "execution_state": state,
                "raw_finish_reason": finish,
                "budget_exhausted": finish == "length",
                "error_category": None if state == "completed" else "fixture_failure",
                "protocol_complete": state == "completed",
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
        if state == "completed":
            store.event(
                "score",
                "scoring",
                key,
                score_case(case, answer, bundle["answer_policy"]),
                monotonic_ns=end + 1,
            )
        for metric, value in [("system_mem_available", 8 * 1024**3), ("service_rss", 7 * 1024**3)]:
            store.sample(
                {
                    "phase": "formal",
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
    stop = (
        "plan_finished"
        if all(state in ("completed", "failed") for state in states)
        else "tool_interrupted"
        if "invalid" in states
        else "cancelled"
    )
    store.event("run_stopped", "finalizing", None, {"reason": stop}, monotonic_ns=cursor + 10**9)
    store.seal()
    store.close()
    return store.path
