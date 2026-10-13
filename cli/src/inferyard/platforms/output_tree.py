"""Operation-scoped directory anchors; never follow a replaced output ancestor."""

import os
from contextlib import contextmanager
from pathlib import Path


class ChangedDirectory(FileExistsError):
    pass


class PosixTree:
    def __init__(self):
        self.handles = []
        self.edges = []

    def descend(self, parent, name, *, create=False, exclusive=False):
        if create:
            try:
                os.mkdir(name, dir_fd=parent)
            except FileExistsError:
                if exclusive:
                    raise
        fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
        self.handles.append(fd)
        if parent is not None:
            self.edges.append((parent, name, fd))
        return fd

    def identity(self, handle):
        info = os.fstat(handle)
        return info.st_dev, info.st_ino

    def check(self):
        for parent, name, fd in self.edges:
            info = os.stat(name, dir_fd=parent, follow_symlinks=False)
            if (info.st_dev, info.st_ino) != self.identity(fd):
                raise ChangedDirectory("output directory changed")

    def file(self, parent, name):
        return os.open(
            name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=parent
        )

    def close(self):
        for fd in reversed(self.handles):
            os.close(fd)


@contextmanager
def anchored(path, identity=None, *, create=False):
    """Hold every ancestor until all writes and path-identity checks have completed."""
    if os.name == "nt":
        from inferyard.platforms.output_tree_windows import WindowsTree

        tree = WindowsTree()
    else:
        tree = PosixTree()
    path = Path(path)
    try:
        handle = tree.descend(None, path.anchor)
        for index, part in enumerate(path.parts[1:]):
            last = index == len(path.parts) - 2
            handle = tree.descend(handle, part, create=create and last, exclusive=create and last)
        if identity is not None and tree.identity(handle) != identity:
            raise ChangedDirectory("output root changed")
        yield tree, handle
        tree.check()
    finally:
        tree.close()
