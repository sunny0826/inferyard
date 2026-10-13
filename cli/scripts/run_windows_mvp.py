"""Own one Windows service, bind/check it, optionally run the benchmark, then close it."""

import argparse
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

import httpx
from collect_windows_machine import collect_machine
from create_windows_candidate import render_toml

from inferyard.config.loader import load_config
from inferyard.platforms.device_preflight import nvidia_snapshot
from inferyard.platforms.identity import (
    hash_file,
    memory_available,
    process_start_ticks,
    resolve_loopback_origin,
)

ROOT = Path(__file__).resolve().parents[1]


def json_file(path, value):
    path.write_text(
        json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="\n"
    )


def prelaunch_budget(config, available):
    minimum = config["output"]["min_available_memory_bytes"]
    model_bytes = Path(config["model"]["local_path"]).stat().st_size
    # Preserve the frozen loaded-model floor and reserve 1 GiB for context/work buffers.
    required = minimum + model_bytes + 1024**3
    return {
        "available_bytes": available,
        "loaded_model_floor_bytes": minimum,
        "model_bytes": model_bytes,
        "startup_reserve_bytes": 1024**3,
        "required_before_start_bytes": required,
        "accepted": available >= required,
    }


def cli_command(arguments):
    packages = Path(sys.prefix) / "Lib/site-packages"
    bootstrap = (
        f"import site,sys; site.addsitedir({str(packages)!r}); "
        "from inferyard.cli import main; raise SystemExit(main(sys.argv[1:]))"
    )
    # Use the base interpreter so the returned PID is the actual CLI worker,
    # avoiding a venv redirector between this owner and the cancellable child.
    return [sys._base_executable, "-c", bootstrap, *arguments]


def command(arguments, out, label, minimum, deadline, gpu_index=None):
    with (
        (out / f"{label}.stdout.json").open("wb") as stdout,
        (out / f"{label}.stderr.log").open("wb") as stderr,
    ):
        child = subprocess.Popen(
            cli_command(arguments),
            stdout=stdout,
            stderr=stderr,
            creationflags=subprocess.CREATE_NO_WINDOW,
        )
        stopped = None
        gpu_due = 0
        try:
            while child.poll() is None:
                if time.monotonic() >= deadline:
                    stopped = "operator_wall_budget_exhausted"
                    break
                if memory_available() < minimum:
                    stopped = "operator_memory_floor_reached"
                    break
                if gpu_index is not None and time.monotonic() >= gpu_due:
                    snapshot = nvidia_snapshot()
                    gpu = next((g for g in snapshot["devices"] if g["index"] == gpu_index), None)
                    with (out / "gpu-samples.jsonl").open("a", encoding="utf-8") as stream:
                        stream.write(
                            json.dumps(
                                {
                                    "phase": label,
                                    "monotonic_ns": time.monotonic_ns(),
                                    "gpu": gpu,
                                    "status": snapshot["status"],
                                }
                            )
                            + "\n"
                        )
                    if gpu is None:
                        stopped = "gpu_observation_unavailable"
                        break
                    if gpu["temperature_c"] is not None and gpu["temperature_c"] >= 85:
                        stopped = "gpu_temperature_limit_reached"
                        break
                    if gpu["memory_free_bytes"] < 512 * 1024**2:
                        stopped = "gpu_free_memory_floor_reached"
                        break
                    gpu_due = time.monotonic() + 1
                time.sleep(0.5)
        finally:
            if child.poll() is None:
                child.terminate()
                child.wait(timeout=10)
    result = {"exit_code": child.returncode, "operator_stop": stopped}
    raw = (out / f"{label}.stdout.json").read_bytes()
    if raw.strip():
        result["result"] = json.loads(raw)
    return result


def operate(config_path, out, *, formal=False, max_wall_seconds=1800):
    if os.name != "nt":
        raise ValueError("native_windows_required")
    out = out.resolve()
    if out.drive.upper() != "D:" or not out.is_relative_to(ROOT):
        raise ValueError("operator_evidence_must_stay_in_D_workspace")
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    gpu_index = None
    if config["engine"].get("backend", "cpu") == "cuda":
        args = config["engine"]["startup_args"]
        device = args[args.index("--device") + 1] if "--device" in args else ""
        match = re.fullmatch(r"CUDA(\d+)", device)
        if match is None:
            raise ValueError("explicit_cuda_device_required")
        gpu_index = int(match[1])
    for section, field, digest_field in (
        ("model", "local_path", "sha256"),
        ("engine", "binary_path", "binary_sha256"),
        ("model", "template_path", "template_sha256"),
    ):
        hash_file(Path(config[section][field]), config[section][digest_field])
    origin, _, _ = resolve_loopback_origin(config["endpoint"]["url"])
    out.mkdir(parents=True, exist_ok=False)
    # Capture the full preparation reference even when the later memory gate blocks startup.
    machine = collect_machine(out / "preparation", config_path=loaded.source)
    if gpu_index is not None:
        json_file(out / "gpu.before.json", nvidia_snapshot())
    budget = prelaunch_budget(config, memory_available())
    record = {
        "kind": "windows_owned_mvp_operator.v1",
        "candidate": str(loaded.source),
        "formal_requested": formal,
        "max_wall_seconds": max_wall_seconds,
        "prelaunch_memory": budget,
        "service_started": False,
        "benchmark_cli_started": False,
        "hardware_qualification_added": False,
        "machine_preparation": machine,
        "requested_backend": config["engine"].get("backend", "cpu"),
        **(
            {
                "gpu_limits": {
                    "index": gpu_index,
                    "temperature_stop_c": 85,
                    "minimum_free_bytes": 512 * 1024**2,
                    "sampling_interval_seconds": 1,
                }
            }
            if gpu_index is not None
            else {}
        ),
    }
    if not budget["accepted"]:
        record.update(
            status="blocked", reason="insufficient_memory_before_model_start", model_requests_sent=0
        )
        json_file(out / "operator.json", record)
        return 2, record
    minimum = config["output"]["min_available_memory_bytes"]
    deadline = time.monotonic() + max_wall_seconds
    json_file(out / "candidate.frozen.json", config)
    with (out / "server.log").open("wb") as log:
        service = subprocess.Popen(
            [config["engine"]["binary_path"], *config["engine"]["startup_args"]],
            cwd=Path(config["engine"]["binary_path"]).parent,
            stdout=log,
            stderr=subprocess.STDOUT,
            creationflags=subprocess.CREATE_NO_WINDOW,
        )
        record.update(service_started=True, server_pid=service.pid)
        code = 2
        try:
            record["process_start_ticks"] = process_start_ticks(service.pid)
            startup_deadline = min(deadline, time.monotonic() + 120)
            with httpx.Client(base_url=origin, trust_env=False, timeout=2) as client:
                while True:
                    if service.poll() is not None:
                        raise RuntimeError("owned_server_exited_before_ready")
                    if memory_available() < minimum:
                        raise RuntimeError("loaded_model_memory_floor_reached")
                    if time.monotonic() >= startup_deadline:
                        raise RuntimeError("owned_server_startup_deadline_reached")
                    try:
                        response = client.get("/health")
                        if response.status_code == 200:
                            break
                    except httpx.HTTPError:
                        pass
                    time.sleep(0.5)
                json_file(out / "health.json", response.json())
                json_file(out / "props.json", client.get("/props").json())
                json_file(out / "slots.before.json", client.get("/slots").json())
            config["endpoint"].update(
                server_pid=service.pid, process_start_ticks=record["process_start_ticks"]
            )
            bound = out / "bound.toml"
            bound.write_text(render_toml(config), encoding="utf-8", newline="\n")
            if gpu_index is not None:
                log.flush()
                text = (out / "server.log").read_text(encoding="utf-8", errors="replace")
                offload = re.findall(r"offloaded (\d+)/(\d+) layers to GPU", text)
                if (
                    not offload
                    or int(offload[-1][0]) != int(offload[-1][1])
                    or int(offload[-1][0]) == 0
                ):
                    raise RuntimeError("full_cuda_model_offload_unverified")
                record["cuda_offload"] = {
                    "layers": int(offload[-1][0]),
                    "total_layers": int(offload[-1][1]),
                    "source": "server.log",
                    "status": "verified",
                }
                json_file(out / "cuda-offload.json", record["cuda_offload"])
            record["benchmark_cli_started"] = True
            record["check"] = command(
                ["check", "--config", str(bound)],
                out,
                "check",
                minimum,
                deadline,
                **({"gpu_index": gpu_index} if gpu_index is not None else {}),
            )
            code = record["check"]["exit_code"]
            if code == 0 and formal:
                record["run"] = command(
                    ["run", "--config", str(bound)],
                    out,
                    "run",
                    minimum,
                    deadline,
                    **({"gpu_index": gpu_index} if gpu_index is not None else {}),
                )
                code = record["run"]["exit_code"]
            record.update(status="completed" if code == 0 else "blocked_or_incomplete")
            with httpx.Client(base_url=origin, trust_env=False, timeout=2) as client:
                json_file(out / "slots.after.json", client.get("/slots").json())
        except RuntimeError as exc:
            code = 2
            record.update(status="blocked", reason=str(exc))
        except httpx.HTTPError, OSError, ValueError:
            code = 4
            record.update(status="operator_error", reason="operator_io_or_protocol_error")
        except KeyboardInterrupt:
            code = 130
            record.update(status="cancelled", reason="operator_cancelled")
        finally:
            if service.poll() is None:
                # This helper owns the process handle. No unrelated service is stopped.
                service.terminate()
                service.wait(timeout=10)
            record["owned_server_closed"] = True
            record["server_exit_code"] = service.returncode
            record["available_memory_after_close_bytes"] = memory_available()
            if gpu_index is not None:
                json_file(out / "gpu.after.json", nvidia_snapshot())
            json_file(out / "operator.json", record)
    return code, record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument(
        "--run", action="store_true", help="run the full benchmark after check succeeds"
    )
    parser.add_argument("--max-wall-seconds", type=int, default=1800)
    args = parser.parse_args()
    if args.max_wall_seconds <= 0:
        parser.error("max-wall-seconds must be positive")
    code, record = operate(
        args.config, args.out, formal=args.run, max_wall_seconds=args.max_wall_seconds
    )
    print(json.dumps(record, ensure_ascii=False))
    return code


if __name__ == "__main__":
    raise SystemExit(main())
