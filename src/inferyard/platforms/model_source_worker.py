"""Private acquisition child. No subprocesses, SDKs, request logging or raw diagnostics."""

import json
import logging
import signal
import sys

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.model_source import ModelSource
from inferyard.platforms.model_source_download import transfer
from inferyard.platforms.model_source_host import Budget


def main():
    logging.disable(logging.CRITICAL)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    try:
        request = json.loads(sys.stdin.buffer.read(32 * 1024))
        details = transfer(
            ModelSource(**request["source"]),
            request["out"],
            request["token_env"],
            Budget(**request["budget"]),
        )
        result = {"details": details}
    except PreparationError as exc:
        result = {"reason": exc.reason}
    except OSError:
        result = {"reason": "io_error"}
    except BaseException:
        result = {"reason": "model_acquire_incomplete"}
    sys.stdout.write(json.dumps(result, allow_nan=False) + "\n")
    sys.stdout.flush()


if __name__ == "__main__":
    main()
