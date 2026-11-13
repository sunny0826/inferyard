"""Worker entrypoint recognition stays shape-based instead of protocol-replay."""

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms.local_engines.workers import validate_python_worker

ROOT = ["/venv/bin/python", "-I", "-S", "-B", "/package/bootstrap.py", "sglang", "--fixture"]


def worker(code, *trailing):
    return [ROOT[0], "-B", "-I", "-S", "-c", code, *trailing]


def test_forked_root_argv_is_accepted():
    validate_python_worker(ROOT, ROOT)


@pytest.mark.parametrize(
    "arguments",
    [
        worker("from multiprocessing.spawn import spawn_main; spawn_main(tracker_fd=5, pipe_handle=6)"),
        worker("from multiprocessing.resource_tracker import main; main(7)"),
        worker(
            "import sys; from multiprocessing.forkserver import main; "
            "main(8, 9, ['/x'], sys_path=['/venv/lib/site'], sys_argv=sys.argv[1:])",
        ),
        # CPython attaches the inherited tail only when the root carried extra arguments.
        worker(
            "from multiprocessing.spawn import spawn_main; spawn_main(tracker_fd=5, pipe_handle=6)",
            "--multiprocessing-fork",
            *ROOT[6:],
        ),
    ],
)
def test_standard_multiprocessing_entrypoints_pass(arguments):
    validate_python_worker(arguments, ROOT)


@pytest.mark.parametrize(
    "code",
    [
        "from os import system; system('x')",
        "from multiprocessing.spawn import spawn_main; spawn_main",
        "from multiprocessing.spawn import spawn_main as sm; sm()",
        "from multiprocessing.spawn import spawn_main; spawn_main(); import sys",
    ],
)
def test_nonstandard_entrypoints_are_unverified(code):
    with pytest.raises(PreflightError, match="python_worker_entrypoint_unverified"):
        validate_python_worker(worker(code), ROOT)


@pytest.mark.parametrize(
    "arguments",
    [
        [ROOT[0], "-B", "-I", "-c", "from multiprocessing.spawn import spawn_main; spawn_main()"],
        [ROOT[0], "-B", "-I", "-S", "-m", "multiprocessing.spawn"],
        ["/other/bin/python", "-B", "-I", "-S", "-c", "from multiprocessing.spawn import spawn_main"],
    ],
)
def test_unisolated_or_foreign_workers_require_isolation(arguments):
    with pytest.raises(PreflightError, match="python_worker_isolation_required"):
        validate_python_worker(arguments, ROOT)
