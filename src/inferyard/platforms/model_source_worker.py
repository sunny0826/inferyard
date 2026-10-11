"""Private acquisition child. No subprocesses, SDKs, request logging or raw diagnostics."""

import json
import logging
import signal
import sys

from inferyard.platforms.model_source import ModelSource
from inferyard.platforms.model_source_budget import Budget
from inferyard.platforms.model_source_download import transfer
from inferyard.platforms.model_source_progress import Progress


def emit(value):
    sys.stdout.write(json.dumps(value, allow_nan=False) + "\n")
    sys.stdout.flush()


def main():
    logging.disable(logging.CRITICAL)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    progress = Progress(emit=emit)
    try:
        request = json.loads(sys.stdin.buffer.read(32 * 1024))
        details = transfer(
            ModelSource(**request["source"]),
            request["out"],
            request["token_env"],
            Budget(**request["budget"]),
            progress=progress,
        )
        result = {"details": details}
    except BaseException as exc:
        failure = progress.failure(exc)
        result = {"reason": failure.reason, "diagnostics": failure.diagnostics}
    emit(result)


if __name__ == "__main__":
    main()
