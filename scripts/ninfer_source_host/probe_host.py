"""Stage46 纯内存诊断宿主。

内部只构造一个已合入的 Custodian，公开方法按原语义代理。
evidence 单独标记 experiment_stage=46，不改库的 stage。
main 无执行许可：不读取参数或环境，不构造宿主，不调用 Backend。
"""

from __future__ import annotations

from copy import deepcopy

from .custody import Custodian

EXPERIMENT_STAGE = 46


def _as_bytes(value: object) -> bytes:
    if type(value) is bytes:
        return value
    raise TypeError("snapshot stream is not bytes")


def _copy_job(job: dict) -> dict:
    return {
        "job_id": job["job_id"],
        "kind": job["kind"],
        "state": job["state"],
        "returncode": job["returncode"],
        "original_exited": job["original_exited"],
        "close_attempts": job["close_attempts"],
        "close_successes": job["close_successes"],
        "stdout": _as_bytes(job["stdout"]),
        "stderr": _as_bytes(job["stderr"]),
        "primary_error": job["primary_error"],
        "cleanup_errors": list(job["cleanup_errors"]),
    }


def _copy_snapshot(snapshot: dict) -> dict:
    return {
        "protocol": snapshot["protocol"],
        "host_id": snapshot["host_id"],
        "execution_sha256": snapshot["execution_sha256"],
        "scripts_sha256": snapshot["scripts_sha256"],
        "stage": snapshot["stage"],
        "jobs": [_copy_job(job) for job in snapshot["jobs"]],
        "custody_required": snapshot["custody_required"],
    }


def _copy_decision(decision: dict) -> dict:
    copied = _copy_snapshot(decision)
    copied["can_exit"] = decision["can_exit"]
    copied["success"] = decision["success"]
    copied["reason"] = decision["reason"]
    return copied


class ProbeHost:
    """同一 Custodian 的诊断代理。resume 继续调用该实例，不另造保管器。"""

    def __init__(self, identity, backend, now_ticks, frequency, grace_ticks):
        self._identity = deepcopy(identity)
        self._custodian = Custodian(identity, backend, now_ticks, frequency, grace_ticks)

    def start(self, job: dict, spec: dict) -> str:
        return self._custodian.start(job, spec)

    def step(self) -> dict:
        return self._custodian.step()

    def resume(self, host_id: str, execution_sha256: str) -> dict:
        return self._custodian.resume(host_id, execution_sha256)

    def cancel(self, job_id: str, reason: str) -> dict:
        return self._custodian.cancel(job_id, reason)

    def snapshot(self) -> dict:
        return self._custodian.snapshot()

    def owned_objects(self) -> tuple:
        return self._custodian.owned_objects()

    def decision(self) -> dict:
        return self._custodian.decision()

    def evidence(self) -> dict:
        decision = _copy_decision(self._custodian.decision())
        snapshot = _copy_snapshot(self._custodian.snapshot())
        return {
            "experiment_stage": EXPERIMENT_STAGE,
            "identity": deepcopy(self._identity),
            "snapshot": snapshot,
            "decision": decision,
            "retained_count": len(self.owned_objects()),
        }


def main(argv: list[str] | None = None) -> int:
    """任何参数都拒绝。调用方负责退出码，这里不启动进程。"""
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
