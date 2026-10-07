"""NInfer 源码获取宿主进程保管器(Stage45,Kimi 份额)。

冻结接口见 docs/contracts/windows-source-host-contract.md。纯内存状态机:仅输入校验、注入时钟采样
和 Backend 调用,无文件 I/O、sleep 或隐藏循环。fake Backend 单测,非 Windows 原生证据。
"""

from __future__ import annotations

import os
import re
from copy import deepcopy

PROTOCOL = "ninfer-source-host/1"
STAGE = 45
PUMP_LIMIT = 4096
OUTPUT_LIMIT = 32768
ARGV_UTF16_LIMIT = 12000

_ERROR_CODES = frozenset(
    "host_identity host_job host_clock host_deadline host_output_limit "
    "host_backend host_cleanup host_cancelled host_busy".split()
)
_CANCEL_REASONS = frozenset({"operator_cancel", "deadline", "output_limit", "io_error"})
_JOB_KINDS = frozenset({"file_io", "asset"})
_IDENTITY_KEYS = frozenset(
    "protocol host_id execution_sha256 scripts_sha256 stage work_end_ticks total_end_ticks".split()
)
_JOB_KEYS = _IDENTITY_KEYS | {"job_id", "kind", "task_end_ticks"}
_SPEC_KEYS = frozenset({"argv", "cwd", "executable_sha256", "script_sha256", "no_descendants"})
_UUID_RE = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
_SHA256_RE = re.compile(r"[0-9a-f]{64}")


class HostError(Exception):
    """输入拒绝或 Backend 失败的稳定原因码;code 属于冻结原因码集合。"""

    def __init__(self, code: str):
        if type(code) is not str or code not in _ERROR_CODES:
            raise ValueError(f"unknown host error code: {code!r}")
        super().__init__(code)
        self.code = code


class BackendError(HostError):
    """Backend 在 start 之后报告的操作失败,code 固定 host_backend。"""

    def __init__(self):
        super().__init__("host_backend")


class BackendStartError(BackendError):
    """start 失败;已创建原对象时 ref 非 None,Custodian 必须接收并保留该引用。"""

    def __init__(self, ref=None, cause=None):
        super().__init__()
        self.ref = ref
        self.cause = cause


def _is_uuid(value: object) -> bool:
    return type(value) is str and _UUID_RE.fullmatch(value) is not None


def _is_sha256(value: object) -> bool:
    return type(value) is str and _SHA256_RE.fullmatch(value) is not None


def _check_identity_fields(identity: dict) -> None:
    # RunIdentity 七字段严格类型与值域;前提:键集合已校验。bool 冒充整数在此拒绝。
    if (
        identity["protocol"] != PROTOCOL
        or not _is_uuid(identity["host_id"])
        or not _is_sha256(identity["execution_sha256"])
        or not _is_sha256(identity["scripts_sha256"])
    ):
        raise HostError("host_identity")
    stage = identity["stage"]
    work = identity["work_end_ticks"]
    total = identity["total_end_ticks"]
    if type(stage) is not int or stage != STAGE:
        raise HostError("host_identity")
    if type(work) is not int or work < 0 or type(total) is not int or total <= work:
        raise HostError("host_identity")


def _check_identity(identity: object) -> None:
    if type(identity) is not dict or set(identity) != _IDENTITY_KEYS:
        raise HostError("host_identity")
    _check_identity_fields(identity)


class _Job:
    """单个被保管进程的可变内部状态;快照由其派生独立副本。"""

    __slots__ = (
        "job_id kind ref state returncode exit_confirmed eof_seen close_attempts "
        "close_successes pipes_attempted pipes_succeeded handle_attempted "
        "handle_succeeded stdout stderr primary_error cleanup_errors "
        "terminate_called terminate_ticks kill_called output_limited task_end_ticks "
        "deadline_noted output_noted exit_ticks".split()
    )

    def __init__(self, job_id: str, kind: str, ref: object, state: str, task_end: int):
        self.job_id, self.kind, self.ref, self.state = job_id, kind, ref, state
        self.task_end_ticks = task_end
        self.returncode = self.primary_error = self.exit_ticks = None
        self.exit_confirmed = self.eof_seen = False
        self.close_attempts = self.close_successes = self.terminate_ticks = 0
        self.pipes_attempted = self.pipes_succeeded = False
        self.handle_attempted = self.handle_succeeded = False
        self.stdout, self.stderr = bytearray(), bytearray()
        self.cleanup_errors = []
        self.terminate_called = self.kill_called = False
        self.output_limited = self.deadline_noted = self.output_noted = False


class Custodian:
    """冻结接口的进程保管器;同实例 resume 核验身份,关闭成功后释放原引用。"""

    def __init__(self, identity, backend, now_ticks, frequency, grace_ticks):
        _check_identity(identity)
        if not callable(now_ticks) or type(frequency) is not int or frequency <= 0:
            raise HostError("host_clock")
        if type(grace_ticks) is not int or grace_ticks < 0:
            raise HostError("host_clock")
        self._identity = deepcopy(identity)
        self._backend = backend
        self._now_ticks = now_ticks
        self._frequency = frequency
        self._grace_ticks = grace_ticks
        self._jobs: list[_Job] = []
        self._job_ids: set[str] = set()
        self._last_ticks: int | None = None

    def _tick(self, job: _Job | None) -> int | None:
        # 采样一次时钟;类型/回退错误记入未关闭 job(稳定,不可恢复成功)并返回 None。
        now = self._now_ticks()
        if (
            type(now) is not int
            or now < 0
            or (self._last_ticks is not None and now < self._last_ticks)
        ):
            if job is not None:
                self._note_error(job, "host_clock")
            return None
        self._last_ticks = now
        return now

    def _sample_now(self) -> int:
        # busy 守卫保证未关闭 job 至多一个;坏时钟下不做任何 Backend 调用。
        active = next((j for j in self._jobs if j.state != "closed"), None)
        now = self._tick(active)
        if now is None:
            raise HostError("host_clock")
        return now

    def _check_job(self, job: object, now: int) -> None:
        if type(job) is not dict or set(job) != _JOB_KEYS:
            raise HostError("host_job")
        if not _is_uuid(job["job_id"]):
            raise HostError("host_job")
        if type(job["kind"]) is not str or job["kind"] not in _JOB_KINDS:
            raise HostError("host_job")
        task = job["task_end_ticks"]
        if type(task) is not int or task <= now or task > self._identity["work_end_ticks"]:
            raise HostError("host_job")
        # 身份投影先满足 RunIdentity 严格类型(拒绝 float/bool 冒充),再逐键相等比对。
        _check_identity_fields(job)
        if any(job[key] != self._identity[key] for key in _IDENTITY_KEYS):
            raise HostError("host_identity")

    @staticmethod
    def _check_spec(spec: object) -> None:
        if type(spec) is not dict or set(spec) != _SPEC_KEYS:
            raise HostError("host_job")
        argv = spec["argv"]
        if type(argv) is not tuple or not argv:
            raise HostError("host_job")
        width = 0
        for item in argv:
            if type(item) is not str or "\0" in item:
                raise HostError("host_job")
            try:
                width += len(item.encode("utf-16-le")) // 2
            except UnicodeEncodeError:
                # 未配对 surrogate 无法编码 UTF-16,稳定拒绝而非泄漏 UnicodeEncodeError。
                raise HostError("host_job") from None
        if width > ARGV_UTF16_LIMIT:
            raise HostError("host_job")
        cwd = spec["cwd"]
        if type(cwd) is not str or not cwd or not os.path.isabs(cwd):
            raise HostError("host_job")
        if not _is_sha256(spec["executable_sha256"]) or not _is_sha256(spec["script_sha256"]):
            raise HostError("host_job")
        if spec["no_descendants"] is not True:
            raise HostError("host_job")

    @staticmethod
    def _note_error(job: _Job, code: str) -> None:
        if job.primary_error is None:
            job.primary_error = code
        else:
            job.cleanup_errors.append(code)

    def _backend_failed(self, job: _Job) -> None:
        # pump/exit_code 轮询失败只在首次进入 unknown 时记一次,之后轮询不再重复记。
        if job.state != "unknown":
            job.state = "unknown"
            self._note_error(job, "host_backend")

    def _signal(self, job: _Job, method: str) -> None:
        try:
            getattr(self._backend, method)(job.ref)
        except BackendError:
            self._note_error(job, "host_backend")
            job.state = "unknown"

    def _terminate(self, job: _Job, now: int) -> None:
        job.terminate_called = True
        job.terminate_ticks = now
        self._signal(job, "terminate")

    def _kill(self, job: _Job) -> None:
        job.kill_called = True
        self._signal(job, "kill")

    def _append_output(self, job: _Job, stream: str, data: bytes) -> None:
        # 只记账:截断保存前缀并置超限标志;记错与停止信号由 _review 统一处理。
        buf = job.stdout if stream == "stdout" else job.stderr
        if len(buf) + len(data) <= OUTPUT_LIMIT:
            buf += data
            return
        buf += data[: OUTPUT_LIMIT - len(buf)]
        job.output_limited = True

    def _pump(self, job: _Job) -> None:
        try:
            out = self._backend.pump(job.ref, PUMP_LIMIT)
        except BackendError:
            self._backend_failed(job)
            return
        if (
            type(out) is not dict
            or type(out.get("stdout")) is not bytes
            or type(out.get("stderr")) is not bytes
            or type(out.get("eof")) is not bool
        ):
            self._backend_failed(job)
            return
        self._append_output(job, "stdout", out["stdout"])
        self._append_output(job, "stderr", out["stderr"])
        if out["eof"]:
            job.eof_seen = True

    def _poll_exit(self, job: _Job) -> None:
        try:
            code = self._backend.exit_code(job.ref)
        except BackendError:
            self._backend_failed(job)
            return
        if code is None:
            return
        if type(code) is not int:
            self._backend_failed(job)
            return
        job.exit_confirmed = True
        job.returncode = code
        if job.state in ("running", "stopping"):
            job.state = "exited"

    def _record_limits(self, job: _Job, now: int) -> None:
        # 纯内存记账(到 total 也执行):任务工作期限取 min(task, work);已确认退出后的
        # 收尾只用 total 期限,退出采样点早于工作期限不记任务超期。
        if job.exit_confirmed and job.exit_ticks is None:
            job.exit_ticks = now
        work_end = min(job.task_end_ticks, self._identity["work_end_ticks"])
        late = now >= work_end and (not job.exit_confirmed or job.exit_ticks >= work_end)
        if late and not job.deadline_noted and job.state != "closed":
            job.deadline_noted = True
            self._note_error(job, "host_deadline")
        if job.output_limited and not job.output_noted:
            job.output_noted = True
            self._note_error(job, "host_output_limit")

    def _stop_if_needed(self, job: _Job, now: int) -> bool:
        # 停止信号与记账分离:已确认退出或已发信号的对象不再 terminate。
        if (
            not job.exit_confirmed
            and not job.terminate_called
            and (job.deadline_noted or job.output_limited)
            and job.state in ("running", "unknown")
        ):
            if job.state == "running":
                job.state = "stopping"
            self._terminate(job, now)
            return self._review(job)
        return True

    def _review(self, job: _Job) -> bool:
        # 每个 Backend 调用返回后的统一边界:复核时钟、期限记账,到 total 停止本轮后续调用。
        now = self._tick(job)
        if now is None:
            return False
        self._record_limits(job, now)
        if now >= self._identity["total_end_ticks"]:
            return False
        return self._stop_if_needed(job, now)

    def _close(self, job: _Job) -> None:
        # close_pipes/close_handle 各至多尝试一次;失败不重试、不释放引用、不称成功;
        # pipes 返回后跨原总期限时不再发起 handle,保留已入账事实与原引用。
        if not job.pipes_attempted:
            job.pipes_attempted = True
            job.close_attempts += 1
            try:
                self._backend.close_pipes(job.ref)
            except BackendError:
                self._note_error(job, "host_cleanup")
                job.state = "unknown"
            else:
                job.pipes_succeeded = True
                job.close_successes += 1
            if not self._review(job):
                return
        if not job.handle_attempted:
            job.handle_attempted = True
            job.close_attempts += 1
            try:
                self._backend.close_handle(job.ref)
            except BackendError:
                self._note_error(job, "host_cleanup")
                job.state = "unknown"
            else:
                job.handle_succeeded = True
                job.close_successes += 1
            # 关闭事实已入账;时钟/期限错误记入快照但不阻止已知成功关闭释放引用。
            self._review(job)
        if job.pipes_succeeded and job.handle_succeeded:
            job.state = "closed"
            job.ref = None

    def _advance(self, job: _Job, now: int) -> None:
        # 原总期限不重置:到 total 后保留对象与未知状态,不再开始任何自动 Backend 调用。
        if now >= self._identity["total_end_ticks"]:
            return
        self._record_limits(job, now)
        if not self._stop_if_needed(job, now):
            return
        if (
            job.terminate_called
            and not job.kill_called
            and not job.exit_confirmed
            and job.state in ("stopping", "unknown")
            and self._last_ticks >= job.terminate_ticks + self._grace_ticks
        ):
            self._kill(job)
            if not self._review(job):
                return
        # 先确认退出再排空:已确认退出后的输出超限只记错,不再 terminate。
        if not job.exit_confirmed:
            self._poll_exit(job)
            if not self._review(job):
                return
        if not job.eof_seen:
            self._pump(job)
            if not self._review(job):
                return
        if job.exit_confirmed and job.eof_seen and job.state != "closed":
            self._close(job)

    def start(self, job: dict, spec: dict) -> str:
        now = self._sample_now()
        self._check_job(job, now)
        if job["job_id"] in self._job_ids:
            raise HostError("host_job")
        if any(j.state != "closed" for j in self._jobs):
            raise HostError("host_busy")
        self._check_spec(spec)
        try:
            ref = self._backend.start(spec)
        except BackendStartError as exc:
            if exc.ref is not None:
                created = _Job(
                    job["job_id"], job["kind"], exc.ref, "unknown", job["task_end_ticks"]
                )
                self._jobs.append(created)
                self._job_ids.add(job["job_id"])
                self._note_error(created, "host_backend")
                self._review(created)
            raise HostError("host_backend") from exc
        except BackendError as exc:
            raise HostError("host_backend") from exc
        if ref is None:
            raise HostError("host_backend")
        created = _Job(job["job_id"], job["kind"], ref, "running", job["task_end_ticks"])
        self._jobs.append(created)
        self._job_ids.add(job["job_id"])
        self._review(created)
        return job["job_id"]

    def step(self) -> dict:
        now = self._sample_now()
        for job in self._jobs:
            if job.state != "closed":
                self._advance(job, now)
        return self._make_snapshot()

    def cancel(self, job_id: str, reason: str) -> dict:
        now = self._sample_now()
        if type(reason) is not str or reason not in _CANCEL_REASONS or type(job_id) is not str:
            raise HostError("host_job")
        job = next((j for j in self._jobs if j.job_id == job_id), None)
        if job is None:
            raise HostError("host_job")
        if (
            job.state in ("running", "unknown")
            and not job.terminate_called
            and not job.exit_confirmed
            and now < self._identity["total_end_ticks"]
        ):
            self._note_error(job, "host_cancelled")
            if job.state == "running":
                job.state = "stopping"
            self._terminate(job, now)
            self._review(job)
        return self._make_snapshot()

    def resume(self, host_id: str, execution_sha256: str) -> dict:
        if (
            type(host_id) is not str
            or type(execution_sha256) is not str
            or host_id != self._identity["host_id"]
            or execution_sha256 != self._identity["execution_sha256"]
        ):
            raise HostError("host_identity")
        return self.step()

    def snapshot(self) -> dict:
        return self._make_snapshot()

    def owned_objects(self) -> tuple:
        return tuple(j.ref for j in self._jobs if j.ref is not None)

    def decision(self) -> dict:
        now = self._sample_now()
        result = self._make_snapshot()
        can_exit = all(j.state == "closed" for j in self._jobs)
        reason = None
        if not self._jobs:
            reason = "host_job"
        else:
            for job in self._jobs:
                if job.primary_error is not None:
                    reason = job.primary_error
                    break
                if job.cleanup_errors:
                    reason = job.cleanup_errors[0]
                    break
            if reason is None:
                work_end = self._identity["work_end_ticks"]
                if not can_exit:
                    reason = "host_deadline" if now >= work_end else "host_busy"
                elif now >= work_end:
                    reason = "host_deadline"
                elif any(job.returncode != 0 for job in self._jobs):
                    reason = "host_job"
        result |= {"can_exit": can_exit, "success": reason is None and can_exit, "reason": reason}
        return result

    def _make_snapshot(self) -> dict:
        jobs = [
            {
                "job_id": job.job_id,
                "kind": job.kind,
                "state": job.state,
                "returncode": job.returncode,
                "original_exited": job.exit_confirmed,
                "close_attempts": job.close_attempts,
                "close_successes": job.close_successes,
                "stdout": bytes(job.stdout),
                "stderr": bytes(job.stderr),
                "primary_error": job.primary_error,
                "cleanup_errors": list(job.cleanup_errors),
            }
            for job in self._jobs
        ]
        custody_required = any(job.state == "unknown" for job in self._jobs) or (
            any(job.state != "closed" for job in self._jobs)
            and self._last_ticks is not None
            and self._last_ticks >= self._identity["total_end_ticks"]
        )
        return {
            "protocol": PROTOCOL,
            "host_id": self._identity["host_id"],
            "execution_sha256": self._identity["execution_sha256"],
            "scripts_sha256": self._identity["scripts_sha256"],
            "stage": STAGE,
            "jobs": jobs,
            "custody_required": custody_required,
        }
