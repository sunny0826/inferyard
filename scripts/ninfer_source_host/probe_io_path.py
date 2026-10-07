"""Stage46 路径关系。只做词法分类，不调用 WinAPI，也不放宽固定根。"""

from __future__ import annotations

from .file_io_codec import IoError, lexical_ancestors
from .probe_io_limits import FILE_ROOT

__all__ = ["contained_ancestors", "path_relation", "require_root"]


def require_root(root):
    if type(root) is not str or root != FILE_ROOT:
        raise IoError("io_path")
    return root


def path_relation(path, root=FILE_ROOT):
    """返回 root / inside / ancestor / outside。root 本身不是可打开的普通文件。"""
    ancestors = lexical_ancestors(path)
    root_parts = lexical_ancestors(root)
    if ancestors == root_parts:
        return "root"
    if len(ancestors) > len(root_parts) and ancestors[: len(root_parts)] == root_parts:
        return "inside"
    if root_parts[: len(ancestors)] == ancestors:
        return "ancestor"
    return "outside"


def contained_ancestors(path, root=FILE_ROOT):
    """只返回 root 内部普通文件的祖先链。第一项是许可根，末项是 leaf。"""
    if path_relation(path, root) != "inside":
        raise IoError("io_path")
    return lexical_ancestors(path)
