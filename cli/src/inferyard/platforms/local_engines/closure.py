"""Read Mach-O load commands without executing libraries or model imports."""

import os
import stat
import subprocess
from pathlib import Path

from inferyard.platforms.identity import PreflightError

_MAGIC = {
    b"\xfe\xed\xfa\xce",
    b"\xce\xfa\xed\xfe",
    b"\xfe\xed\xfa\xcf",
    b"\xcf\xfa\xed\xfe",
    b"\xca\xfe\xba\xbe",
    b"\xbe\xba\xfe\xca",
    b"\xca\xfe\xba\xbf",
    b"\xbf\xba\xfe\xca",
}
_CACHE = {}


def mapped_native_image(path, stamp):
    """lsof txt also includes data mappings; inspect bytes against the mapped inode."""

    def identity(info):
        return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns

    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or (before.st_dev, before.st_ino) != stamp:
                raise PreflightError("local_engine_mapping_unverified")
            magic = stream.read(4)
            after = os.fstat(stream.fileno())
        if identity(before) != identity(after) or identity(path.stat()) != identity(after):
            raise PreflightError("local_engine_mapping_unverified")
        return magic in _MAGIC
    except FileNotFoundError, PermissionError:
        return None
    except OSError as exc:
        raise PreflightError("local_engine_mapping_unverified") from exc


def _commands(path):
    stat = path.stat()
    key = (str(path), stat.st_ino, stat.st_size, stat.st_mtime_ns, stat.st_ctime_ns)
    if key in _CACHE:
        return _CACHE[key]
    with path.open("rb") as stream:
        if stream.read(4) not in _MAGIC:
            _CACHE[key] = ([], [])
            return [], []
    try:
        result = subprocess.run(
            ["/usr/bin/otool", "-l", str(path)], capture_output=True, text=True, timeout=10
        )
        if result.returncode:
            raise PreflightError("local_engine_macho_unreadable")
        dependencies, rpaths, command = [], [], None
        for line in result.stdout.splitlines():
            row = line.strip()
            if row.startswith("cmd "):
                command = row[4:]
            elif command == "LC_RPATH" and row.startswith("path "):
                rpaths.append(row[5:].rsplit(" (offset", 1)[0])
            elif command in {
                "LC_LOAD_DYLIB",
                "LC_LOAD_WEAK_DYLIB",
                "LC_REEXPORT_DYLIB",
                "LC_LOAD_UPWARD_DYLIB",
            } and row.startswith("name "):
                dependencies.append(row[5:].rsplit(" (offset", 1)[0])
        _CACHE[key] = dependencies, rpaths
        return dependencies, rpaths
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise PreflightError("local_engine_macho_unreadable") from exc


def _system(value):
    return value.startswith(("/usr/lib/", "/System/Library/"))


def _expand(value, path, executable):
    return value.replace("@loader_path", str(path.parent)).replace(
        "@executable_path", str(executable.parent)
    )


def verify_libraries(paths, executable, *, mapped_paths=None):
    """Record unavailable optional libraries; require closure for every mapped file."""
    known = {p.resolve() for p in paths}
    executable = executable.resolve()
    mapped = None if mapped_paths is None else {p.resolve() for p in mapped_paths}
    _, executable_rpaths = _commands(executable)
    inherited = {Path(_expand(r, p, executable)) for p in paths for r in _commands(p)[1]}
    unresolved = []
    for path in paths if mapped is None else sorted(mapped):
        dependencies, rpaths = _commands(path)
        for dependency in dependencies:
            if _system(dependency):
                continue
            if dependency.startswith("@rpath/"):
                candidates = [Path(_expand(r, path, executable)) / dependency[7:] for r in rpaths]
                candidates += [
                    Path(_expand(r, executable, executable)) / dependency[7:]
                    for r in executable_rpaths
                ]
                resolved = {p.resolve() for p in candidates if p.is_file()}
                if not resolved:
                    resolved = {
                        (root / dependency[7:]).resolve()
                        for root in inherited
                        if (root / dependency[7:]).is_file()
                    }
                if mapped is not None:
                    loaded = {p for p in mapped if p.name == Path(dependency).name}
                    if loaded:
                        resolved = loaded
            else:
                target = Path(_expand(dependency, path, executable))
                resolved = {target.resolve()} if target.is_absolute() and target.exists() else set()
            if len(resolved) == 1 and (
                next(iter(resolved)) in known or _system(str(next(iter(resolved))))
            ):
                continue
            reason = "unresolved" if not resolved else "ambiguous_or_outside_inventory"
            if mapped is not None:
                raise PreflightError("local_engine_mapped_library_closure_unverified")
            unresolved.append(
                {"path": str(path.resolve()), "dependency": dependency, "reason": reason}
            )
    return sorted(unresolved, key=lambda row: (row["path"], row["dependency"]))
