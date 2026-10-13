"""Task cancellation on POSIX loops and the Windows Proactor loop."""

import asyncio
import signal


def install_termination_handler():
    loop, task = asyncio.get_running_loop(), asyncio.current_task()
    previous = signal.getsignal(signal.SIGTERM)
    try:
        loop.add_signal_handler(signal.SIGTERM, task.cancel)
    except NotImplementedError:
        # ProactorEventLoop cannot add Unix signal handlers. Console Ctrl+C is
        # handled by asyncio.Runner; Python-delivered SIGTERM uses this fallback.
        signal.signal(signal.SIGTERM, lambda *_: loop.call_soon_threadsafe(task.cancel))

        def restore():
            signal.signal(signal.SIGTERM, previous)

    else:

        def restore():
            loop.remove_signal_handler(signal.SIGTERM)
            signal.signal(signal.SIGTERM, previous)

    return restore
