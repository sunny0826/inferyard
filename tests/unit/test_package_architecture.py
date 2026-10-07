"""Keep the CLI boundary separate from reusable measurement and evidence code."""

import ast
import subprocess
import sys
from pathlib import Path

from inferyard import config, contracts
from inferyard.config.loader import LoadedConfig
from inferyard.contracts.validation import ContractError, Document


def test_public_package_apis_preserve_one_type_identity():
    assert config.LoadedConfig is LoadedConfig
    assert contracts.ContractError is ContractError
    assert contracts.Document is Document


def test_business_modules_do_not_import_cli_presentation():
    root = Path(__file__).parents[2] / "src/inferyard"
    offenders = []
    for path in root.rglob("*.py"):
        if path.is_relative_to(root / "cli") or path.name == "__main__.py":
            continue
        for node in ast.walk(ast.parse(path.read_text())):
            if isinstance(node, ast.ImportFrom):
                imports = [node.module or ""]
            elif isinstance(node, ast.Import):
                imports = [alias.name for alias in node.names]
            else:
                continue
            if any(
                name == "inferyard.cli" or name.startswith("inferyard.cli.") for name in imports
            ):
                offenders.append(str(path.relative_to(root)))
    assert offenders == []


def test_schema_registry_does_not_import_business_packages():
    script = """
import importlib.abc
import sys

allowed = ("inferyard.contracts",)

class RejectBusiness(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if fullname.startswith("inferyard.") and not any(
            fullname == prefix or fullname.startswith(prefix + ".") for prefix in allowed
        ):
            raise AssertionError("schema registry imported business package: " + fullname)

sys.meta_path.insert(0, RejectBusiness())
from inferyard.contracts.schemas import export_schema, schemas_for
assert "experiment" in schemas_for()
assert export_schema("experiment")["$id"] == "urn:local-ai-bench:schema:v3:experiment"
"""
    completed = subprocess.run(
        [sys.executable, "-c", script], capture_output=True, text=True, check=False
    )
    assert completed.returncode == 0, (completed.stdout, completed.stderr)
