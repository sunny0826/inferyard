"""Isolated host lifecycle fixtures; never select the machine's fixed roots."""

import inferyard.runtime.lock as locking


def initialize():
    assert locking.STATE_PATH.parent != locking._HOST_ROOT
    assert locking.LOCK_PATH.parent != locking._HOST_ROOT
    with locking.HostLock():
        pass
