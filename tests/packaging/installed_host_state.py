"""DESTRUCTIVE TO RUNNER STATE: only disposable GitHub-hosted CI, never operator machines.

Uses installed package real fixed lock paths. Leaves crash dirty evidence intact for disposal.
No model service, engine binary, download, or product lock-path switch.
"""

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


def guard():
    if not (
        os.environ.get("GITHUB_ACTIONS") == "true"
        and os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
        and os.environ.get("LAB_DISPOSABLE_HOST_TEST") == "yes"
    ):
        raise RuntimeError("requires_explicit_disposable_github_hosted_runner")


def child(mode):
    from inferyard.platforms.identity import PreflightError, process_start_ticks
    from inferyard.runtime.lock import HostLock

    if mode == "contend":
        try:
            with HostLock():
                raise AssertionError("concurrent installed process acquired host lock")
        except PreflightError as exc:
            assert str(exc) == "host_lock_unavailable"
        return
    with HostLock() as lock:
        assert not lock.state or not lock.state.get("dirty"), "runner has prior dirty evidence"
        lock.dirty(
            "synthetic-installed-crash",
            {
                "url": "http://127.0.0.1:1",
                "server_pid": os.getpid(),
                "process_start_ticks": process_start_ticks(os.getpid()),
            },
            "synthetic-request-marker",
        )
        print("dirty-persisted", flush=True)
        assert sys.stdin.readline().strip() == "crash"
        os._exit(77)  # Deliberate process crash; never remove host state or lock files.


def main():
    guard()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--child", choices=("hold", "contend"))
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    if args.child:
        child(args.child)
        return
    from inferyard.platforms.identity import PreflightError, process_start_ticks
    from inferyard.runtime.lock import HostLock

    argv = [sys.executable, "-I", str(Path(__file__).resolve()), "--child"]
    process = subprocess.Popen(
        [*argv, "hold"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
    )
    try:
        assert process.stdout.readline().strip() == "dirty-persisted"
        subprocess.run([*argv, "contend"], check=True, timeout=15)
        process.communicate("crash\n", timeout=15)
        assert process.returncode == 77
        with HostLock() as lock:
            assert lock.state["dirty"] is True
            before = dict(lock.state)
            endpoint = {
                "server_pid": os.getpid(),
                "process_start_ticks": process_start_ticks(os.getpid()),
            }
            try:
                lock.verify_manual_recovery("wrong", "synthetic", endpoint)
            except PreflightError as exc:
                assert str(exc) == "invalid_recovery_confirmation"
            else:
                raise AssertionError("wrong dirty token accepted")
            proof = lock.verify_manual_recovery(
                before["dirty_token"], "CI crash injection", endpoint
            )
            assert proof["old_process_gone"] is True and lock.state == before
        result = {
            "kind": "installed_host_state.v1",
            "synthetic": True,
            "fixed_paths": True,
            "lock_contention": "passed",
            "crash_dirty": "passed",
            "bound_recovery": "passed",
            "dirty_preserved": True,
            "model_requests_sent": 0,
            "full_installed_request_chain": "not_run",
        }
        with args.out.open("x", encoding="utf-8") as stream:
            json.dump(result, stream, indent=2)
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=15)


if __name__ == "__main__":
    main()
