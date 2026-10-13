"""原 task/total 期限。坏时钟粘滞否决成功，到原 total 后不再开始新的收尾动作。"""

from __future__ import annotations

from .probe_fixture_clock import ClockError


class Deadline:
    """准备、编码、解析、身份和最终决定都属原 task。新工作只在 task 内。

    失败收尾和失败记录只在原 total 内。每次动作返回后采样，下一次开始前用这次结果。
    越过 task 后否决一直粘着，只能收尾。坏时钟或越过 total 之后零新副作用。
    """

    def __init__(self, clock):
        self.clock = clock
        self.veto = False
        self.past_total = False
        self.sticky = False
        self.sampled = None
        self.cause = None

    def allow(self):
        if self.sticky or self.past_total or self.veto:
            return False
        self.after()
        return not self.veto

    def allow_cleanup(self):
        """失败收尾只用原 total。已经越过 total，或时钟已坏，就不再关闭。"""
        if self.sticky or self.past_total:
            return False
        self.after()
        return not self.sticky and not self.past_total

    def after(self):
        if self.sticky:
            self.veto = True
            return
        try:
            now = self.clock()
        except (ClockError, OSError) as exc:
            # bool、倒退和 QueryPerformanceCounter 失败都永久停用。不在这里恢复执行。
            self._stick(exc)
            raise
        self.sampled = now
        if now >= self.clock.total:
            self.past_total = True
            self.veto = True
        elif now >= self.clock.task:
            self.veto = True

    def work_open(self):
        """采样后，仍在原 task 内才可以开始下一次工作。"""
        self.after()
        return not self.sticky and not self.veto and not self.past_total

    def cleanup_open(self):
        """采样后，未到原 total 且时钟未坏，才可以开始失败收尾。"""
        self.after()
        return not self.blocked()

    def note_exception(self, exc):
        if isinstance(exc, ClockError):
            self._stick(exc)

    def _stick(self, exc):
        self.sticky = True
        self.veto = True
        if self.cause is None:
            self.cause = exc

    def blocked(self):
        """原 total 已过，或时钟已坏。此后不能再开始发布、flush 或关闭。"""
        return self.sticky or self.past_total
