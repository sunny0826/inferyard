#!/usr/bin/env python3
"""Optional local reproduction entry; another operator and signoff are not required."""

import argparse
import json
import os
import shlex
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import UTC, datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PYTHON = Path(sys.executable)
CLI = [str(PYTHON), "-m", "inferyard"]


def save(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")


def stop(process):
    """Only signal a child session created by this invocation."""
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=20)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=10)
    return process.returncode


def command(args, out, name, timeout=120):
    started = time.monotonic()
    with (out / f"{name}.stdout").open("w") as stdout:
        with (out / f"{name}.stderr").open("w") as stderr:
            process = subprocess.Popen(
                args, cwd=ROOT, stdout=stdout, stderr=stderr, start_new_session=True
            )
            reason = None
            try:
                while True:
                    try:
                        return process.wait(timeout=min(30, timeout))
                    except subprocess.TimeoutExpired:
                        elapsed = time.monotonic() - started
                        print(f"{name}：已运行 {elapsed:.0f} 秒；日志：{out}", flush=True)
                        if elapsed >= timeout:
                            reason = "wrapper_timeout"
                            raise TimeoutError(f"{name} 超过 {timeout} 秒，停止且不重试") from None
            except KeyboardInterrupt:
                reason = "operator_interrupted"
                raise
            finally:
                code = stop(process)
                save(out / f"{name}.exit.json", {"exit_code": code, "reason": reason})


def verify(out, packet, source_sha256, plan_sha256):
    code = command(
        [
            str(PYTHON),
            "-c",
            "from inferyard.provenance import tool_source_hash; print(tool_source_hash())",
        ],
        out,
        "source",
    )
    if code or (out / "source.stdout").read_text().strip() != source_sha256:
        raise RuntimeError("源码不匹配冻结交接包，请重新准备计划")
    code = command(
        CLI + ["plan", "--config", str(packet / "input/experiment.json"), "--dry-run"],
        out,
        "preview",
    )
    result = json.loads((out / "preview.stdout").read_text())
    details = result["details"]
    frozen = json.loads((packet / "frozen/plan.json").read_text())
    if code or details["plan_sha256"] != plan_sha256 or frozen["plan_sha256"] != plan_sha256:
        raise RuntimeError("计划哈希不匹配")
    safety = frozen["experiment"]["safety"]
    if safety["max_temperature_celsius"] is not None or safety["require_temperature"]:
        raise RuntimeError("新版验收计划不应包含温度门禁")
    if details["request_limit"] != 40 or len(details["trials"]) != 1:
        raise RuntimeError("计划必须为单轮 40 题")
    binding = frozen["runtime_bindings"][0]["config"]
    config = json.loads((packet / "frozen" / binding["path"]).read_text())
    if config["conditions"].get("require_epp_match", True):
        raise RuntimeError("新版验收计划不应要求 EPP 一致")
    return shlex.split((packet / "service-command.txt").read_text())


def record_start_temperature(out):
    from inferyard.platforms.sensors_linux import LinuxSensors

    try:
        sensors = LinuxSensors()
        sensors.sources = [s for s in sensors.sources if s["metric_name"] == "temperature"]
        samples = sensors.collect("operator_start_observation", None)
        save(out / "start-temperature.json", {"samples": samples, "used_as_gate": False})
    except (OSError, ValueError) as exc:
        save(
            out / "start-temperature.json",
            {
                "samples": [],
                "used_as_gate": False,
                "missing_reason": type(exc).__name__,
            },
        )


def ready(process):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError("模型服务启动失败，请查看 service.stderr")
        try:
            with opener.open("http://127.0.0.1:48857/health", timeout=2) as response:
                if response.status == 200:
                    return
        except urllib.error.URLError, TimeoutError:
            pass
        time.sleep(1)
    raise TimeoutError("模型服务 120 秒未就绪")


def run_service(args, out, packet):
    # Refuse occupied ports; runtime additionally verifies the supplied service PID.
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 48857))
    record_start_temperature(out)
    with (out / "service.stdout").open("w") as stdout:
        with (out / "service.stderr").open("w") as stderr:
            process = subprocess.Popen(
                args, cwd=ROOT, stdout=stdout, stderr=stderr, start_new_session=True
            )
            save(out / "service.json", {"pid": process.pid, "argv": args})
            try:
                print("正在启动模型服务（最多等待 120 秒）…", flush=True)
                ready(process)
                return command(
                    CLI
                    + [
                        "run",
                        "--plan",
                        str(packet / "frozen/plan.json"),
                        "--endpoint-url",
                        "http://127.0.0.1:48857",
                        "--server-pid",
                        str(process.pid),
                        "--handoff-note",
                        "真实操作者通过一键入口启动服务；身份由运行时核验",
                        "--output-root",
                        str(out / "batch"),
                    ],
                    out,
                    "run",
                    timeout=8100,
                )
            finally:
                save(out / "service-stop.json", {"pid": process.pid, "exit_code": stop(process)})


def offline(out, signoff):
    result = json.loads((out / "run.stdout").read_text())
    runs = result.get("details", {}).get("runs", [])
    if len(runs) != 1:
        raise RuntimeError("没有唯一实际 run；保留日志，本次无法构建报告")
    run = Path(runs[0]).resolve()
    if not run.is_relative_to(out / "batch/runs"):
        raise RuntimeError("run 路径不属于本次输出")
    signoff["run_id"] = result.get("run_id")
    save(out / "signoff.json", signoff)
    checks = {}
    for kind in ("report", "export"):
        target = out / kind
        checks[f"{kind}_create"] = command(
            CLI + [kind, "--run", str(run), "--out", str(target)], out, kind
        )
        if checks[f"{kind}_create"] == 0:
            checks[f"{kind}_check"] = command(
                CLI + [f"{kind}-check", "--run", str(target)], out, f"{kind}-check"
            )
    save(
        out / "automatic-checks.json",
        {
            "checks": checks,
            "run_status": result.get("status"),
            "completeness": result.get("completeness"),
            "last_stop_reason": result.get("details", {}).get("last_stop_reason"),
            "independent_operator_requirement": "cancelled_by_user",
        },
    )
    return len(checks) == 4 and all(code == 0 for code in checks.values())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--packet", required=True, type=Path, help="frozen reproduction packet")
    parser.add_argument("--source-sha256", required=True, help="expected measurement source hash")
    parser.add_argument("--plan-sha256", required=True, help="expected frozen plan hash")
    parser.add_argument("--out", required=True, type=Path, help="new output directory")
    parser.add_argument("--check", action="store_true", help="只核对交接包，不启动模型")
    parser.add_argument("--operator", help="实际操作者姓名")
    options = parser.parse_args()
    packet = options.packet.resolve()
    out = options.out.resolve()
    operator = options.operator or "local-operator"
    out.mkdir(parents=True, exist_ok=False)
    print(f"本次输出：{out}", flush=True)
    signoff = json.loads((packet / "signoff-template.json").read_text())
    signoff["operator"] = operator
    signoff["status"] = "not_required_under_user_amended_scope"
    signoff["independent_operator_requirement"] = "cancelled_by_user"
    save(out / "signoff.json", signoff)
    status = {
        "started_at": datetime.now(UTC).isoformat(),
        "check_only": options.check,
        "independent_operator_requirement": "cancelled_by_user",
    }
    code = 1
    try:
        args = verify(out, packet, options.source_sha256, options.plan_sha256)
        if options.check:
            print("交接包核对通过；未启动模型。")
            code = 0
        else:
            run_code = run_service(args, out, packet)
            checked = offline(out, signoff)
            code = 0 if run_code == 0 and checked else 1
            print(f"自动步骤结束，run 退出码={run_code}，报告/导出核验={checked}。")
            print(f"本地报告：{out / 'report/report.html'}；另一操作者复现及签收已取消。")
            print("完整度由原始账本决定，自动核验不改写失败或未执行状态。")
    except (Exception, KeyboardInterrupt) as exc:
        code = 130 if isinstance(exc, KeyboardInterrupt) else 1
        status["error"] = f"{type(exc).__name__}: {exc}"
        print(f"本次停止：{status['error']}；证据已保留，不自动重试。", file=sys.stderr)
    finally:
        status.update(exit_code=code, finished_at=datetime.now(UTC).isoformat())
        save(out / "wrapper-status.json", status)
    return code


if __name__ == "__main__":

    def interrupted(_signum, _frame):
        # Ignore further signals while child processes are being cleaned up.
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise KeyboardInterrupt

    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    raise SystemExit(main())
