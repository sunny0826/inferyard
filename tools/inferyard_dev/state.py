"""Source identity and private, atomic development state."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import tempfile
from contextlib import contextmanager
from dataclasses import asdict, dataclass
from pathlib import Path


class DevError(RuntimeError):
    pass


def read_json(path: Path) -> dict:
    if path.is_symlink():
        raise DevError(f"Development state must not be a symlink: {path}")

    def pairs(entries):
        result = {}
        for key, value in entries:
            if key in result:
                raise DevError(f"Duplicate JSON field in {path.name}: {key}")
            result[key] = value
        return result

    try:
        value = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=pairs)
    except (OSError, ValueError) as exc:
        raise DevError(f"Cannot read development state: {path}") from exc
    if not isinstance(value, dict):
        raise DevError(f"Expected a JSON object: {path}")
    return value


def private_dir(path: Path) -> Path:
    if path.is_symlink():
        raise DevError(f"Development directory must not be a symlink: {path}")
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    path.chmod(0o700)
    return path


def write_json(path: Path, value: dict, *, replace: bool = True) -> None:
    private_dir(path.parent)
    if not replace and path.exists():
        raise DevError(f"Refusing to replace existing development evidence: {path}")
    fd, name = tempfile.mkstemp(prefix=f".{path.name}-", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        if replace:
            os.replace(name, path)
        else:
            os.link(name, path)
    finally:
        Path(name).unlink(missing_ok=True)


def inside(path: Path, root: Path) -> Path:
    base = root.resolve()
    try:
        parts = path.absolute().relative_to(base).parts
    except ValueError as exc:
        raise DevError(f"Development path is outside its root: {path}") from exc
    cursor = base
    for part in parts:
        cursor = cursor / part
        if cursor.is_symlink():
            raise DevError(f"Development path must not traverse a symlink: {path}")
    result = path.resolve()
    if not result.is_relative_to(base) or result == base:
        raise DevError(f"Development path is outside its root: {path}")
    return result


def file_sha(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def content_sha(root: Path, files: list[Path]) -> str:
    digest = hashlib.sha256()
    for path in sorted(files, key=lambda item: item.relative_to(root).as_posix()):
        if path.is_symlink():
            raise DevError(f"Source/package files must not be symlinks: {path}")
        digest.update(path.relative_to(root).as_posix().encode("utf-8") + b"\0")
        with path.open("rb") as stream:
            while chunk := stream.read(1024 * 1024):
                digest.update(chunk)
        digest.update(b"\0")
    return digest.hexdigest()


def package_sha(root: Path) -> str:
    files = [
        path
        for path in root.rglob("*")
        if path.is_file()
        and "__pycache__" not in path.relative_to(root).parts
        and path.suffix not in {".pyc", ".pyo"}
    ]
    if not files:
        raise DevError("Installed CLI package is empty")
    return content_sha(root, files)


@dataclass(frozen=True)
class Source:
    checkout: str
    branch: str
    commit: str
    dirty: bool
    source_sha256: str

    @property
    def profile_key(self) -> str:
        return hashlib.sha256(self.branch.encode()).hexdigest()[:20]

    def fields(self) -> dict:
        return asdict(self)


SOURCE_PATHS = (
    "cli/src",
    "cli/pyproject.toml",
    "cli/uv.lock",
    "cli/README.md",
    "cli/LICENSE",
    "studio/*.go",
    "studio/internal",
    "studio/go.mod",
    "studio/go.sum",
    "studio/mygo.config.ts",
    "tools/dev.py",
    "tools/inferyard_dev",
    "mise.toml",
)


def git(root: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess:
    result = subprocess.run(
        ["git", "-C", str(root), *args],
        capture_output=True,
        check=False,
    )
    if check and result.returncode:
        raise DevError("Cannot inspect the checkout with Git")
    return result


def source(root: Path) -> Source:
    branch = git(root, "symbolic-ref", "--quiet", "--short", "HEAD", check=False)
    commit = git(root, "rev-parse", "HEAD").stdout.decode().strip()
    name = branch.stdout.decode().strip() if branch.returncode == 0 else f"detached-{commit}"
    listing = git(
        root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", *SOURCE_PATHS
    ).stdout
    paths = {root / os.fsdecode(raw) for raw in listing.split(b"\0") if raw}
    files = [path for path in paths if path.is_file() and not path.name.endswith("_test.go")]
    dirty = bool(
        git(root, "status", "--porcelain", "--untracked-files=all", "--", *SOURCE_PATHS).stdout
    )
    return Source(str(root), name, commit, dirty, content_sha(root, files))


@contextmanager
def launcher_lock(root: Path):
    private_dir(root)
    path = root / "launcher.lock"
    if path.is_symlink():
        raise DevError("Unsafe development launcher lock")
    with path.open("a+b") as stream:
        path.chmod(0o600)
        if os.name == "nt":
            import msvcrt

            if not path.stat().st_size:
                stream.write(b"\0")
                stream.flush()
            stream.seek(0)
            try:
                msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError as exc:
                raise DevError("Another development command is running") from exc
        else:
            import fcntl

            try:
                fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError as exc:
                raise DevError("Another development command is running") from exc
        try:
            yield
        finally:
            if os.name == "nt":
                stream.seek(0)
                msvcrt.locking(stream.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(stream.fileno(), fcntl.LOCK_UN)
