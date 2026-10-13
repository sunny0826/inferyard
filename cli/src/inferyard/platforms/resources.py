"""Select native resource collection without loading platform libraries eagerly."""

import sys
from pathlib import Path

from inferyard.platforms.identity import PreflightError


def resource_collector_id(*, proc_root=Path("/proc")):
    if sys.platform == "darwin" and proc_root == Path("/proc"):
        return "macos-resource.v1"
    if sys.platform == "win32" and proc_root == Path("/proc"):
        return "windows-resource.v1"
    if sys.platform.startswith("linux") or proc_root != Path("/proc"):
        return "linux-resource.v2"
    raise PreflightError("resource_collector_platform_unsupported")


def ResourceSampler(store, config, **kwargs):
    collector = resource_collector_id(proc_root=kwargs.get("proc_root", Path("/proc")))
    if collector == "macos-resource.v1":
        from inferyard.platforms.resources_macos import ResourceSampler as NativeSampler
    elif collector == "windows-resource.v1":
        from inferyard.platforms.resources_windows import ResourceSampler as NativeSampler
    else:
        from inferyard.platforms.resources_linux import ResourceSampler as NativeSampler
    return NativeSampler(store, config, **kwargs)
