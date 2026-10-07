"""Bounded subprocess reads sharing one wall-clock deadline per observation."""

import os
import selectors
import signal
import subprocess
import time

from inferyard.platforms.identity import PreflightError

TOTAL_TIMEOUT = 10.0


def remaining(deadline, *, cap=2.0, prefix="engine_fit_lms_observer"):
    duration = min(cap, deadline - time.monotonic())
    if duration <= 0:
        raise PreflightError(prefix + "_timeout")
    return duration


def read_process(command, *, deadline, limit, prefix, capture_stderr=False):
    deadline = min(deadline, time.monotonic() + 2.0)
    remaining(deadline, prefix=prefix)
    try:
        with subprocess.Popen(
            command,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE if capture_stderr else subprocess.DEVNULL,
            env={**os.environ, "NO_COLOR": "1"},
            start_new_session=True,
        ) as process:
            try:
                output, errors = bytearray(), bytearray()
                with selectors.DefaultSelector() as selector:
                    selector.register(process.stdout, selectors.EVENT_READ, output)
                    if capture_stderr:
                        selector.register(process.stderr, selectors.EVENT_READ, errors)
                    while selector.get_map():
                        events = selector.select(remaining(deadline, prefix=prefix))
                        if not events:
                            raise PreflightError(prefix + "_timeout")
                        for key, _ in events:
                            chunk = os.read(
                                key.fd, min(65536, limit + 1 - len(output) - len(errors))
                            )
                            if not chunk:
                                selector.unregister(key.fileobj)
                                continue
                            key.data.extend(chunk)
                            if len(output) + len(errors) > limit:
                                raise PreflightError(prefix + "_output_too_large")
                code = process.wait(timeout=remaining(deadline, prefix=prefix))
                return code, bytes(output), bytes(errors)
            finally:
                # Target only the private observer group. Some sandbox policies
                # forbid killpg even for our children, while permitting kill.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                except PermissionError:
                    if process.poll() is None:
                        process.kill()
                process.wait()
    except subprocess.TimeoutExpired as exc:
        raise PreflightError(prefix + "_timeout") from exc
    except OSError as exc:
        raise PreflightError(prefix + "_unreadable") from exc
