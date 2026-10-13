"""ProbeHost 源码形状：控制路径不得出现文件、sleep、线程、网络或隐藏循环。"""

import ast
from pathlib import Path

HOST_DIR = Path(__file__).resolve().parents[2] / "scripts" / "ninfer_source_host"
FORBIDDEN_CALLS = {
    "open",
    "sleep",
    "Thread",
    "Popen",
    "run",
    "urlopen",
    "system",
    "popen",
    "mkdir",
    "remove",
    "unlink",
    "exec",
    "eval",
    "compile",
    "__import__",
}
FORBIDDEN_STATEMENTS = (ast.While, ast.For, ast.AsyncFor, ast.With, ast.AsyncWith)


def _call_name(node):
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        return node.attr
    return ""


def _host_sources():
    found = sorted(HOST_DIR.glob("probe_host.py")) + sorted(HOST_DIR.glob("probe_host_*.py"))
    assert [path.name for path in found] == ["probe_host.py"]
    return found


def _function(body, name):
    return next(node for node in body if isinstance(node, ast.FunctionDef) and node.name == name)


def test_probe_host_ast_has_no_io_sleep_thread_or_network():
    for path in _host_sources():
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        imports = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imports.extend(alias.name.split(".")[0] for alias in node.names)
            elif isinstance(node, ast.ImportFrom):
                imports.append(node.module or "")
            elif isinstance(node, ast.Call):
                assert _call_name(node.func) not in FORBIDDEN_CALLS
            assert not isinstance(node, FORBIDDEN_STATEMENTS)
        assert [name for name in imports if name != "__future__"] == ["copy", "custody"]
        main_fn = _function(tree.body, "main")
        body = [node for node in main_fn.body if not isinstance(node, ast.Expr)]
        assert len(body) == 1
        assert isinstance(body[0], ast.Return)
        assert isinstance(body[0].value, ast.Constant)
        assert body[0].value.value == 2
        host_cls = next(node for node in tree.body if isinstance(node, ast.ClassDef))
        resume = _function(host_cls.body, "resume")
        resume_builds = [
            node
            for node in ast.walk(resume)
            if isinstance(node, ast.Call) and _call_name(node.func) == "Custodian"
        ]
        assert resume_builds == []
        init = _function(host_cls.body, "__init__")
        constructions = [
            node
            for node in ast.walk(init)
            if isinstance(node, ast.Call) and _call_name(node.func) == "Custodian"
        ]
        assert len(constructions) == 1
