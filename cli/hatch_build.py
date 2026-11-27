"""Hatch build hook to include root benchmarks/ into wheel and sdist artifacts."""

from pathlib import Path

from hatchling.builders.hooks.plugin.interface import BuildHookInterface


class CustomBuildHook(BuildHookInterface):
    def initialize(self, version, build_data):
        root = Path(self.root)
        candidates = (root / "benchmarks", root.parent / "benchmarks")
        source = next((path for path in candidates if (path / "catalog.json").is_file()), None)
        if source is None:
            raise FileNotFoundError("benchmarks/catalog.json not found for packaging")

        force_include = build_data.setdefault("force_include", {})
        if self.target_name == "wheel":
            prefix = "inferyard/data/benchmarks"
            force_include[str(source / "catalog.json")] = f"{prefix}/catalog.json"
            for bundle_file in sorted((source / "bundles").glob("*.json")):
                force_include[str(bundle_file)] = f"{prefix}/bundles/{bundle_file.name}"
        elif self.target_name == "sdist":
            force_include[str(source / "catalog.json")] = "benchmarks/catalog.json"
            readme = source / "README.md"
            if readme.is_file():
                force_include[str(readme)] = "benchmarks/README.md"
            for bundle_file in sorted((source / "bundles").glob("*.json")):
                force_include[str(bundle_file)] = f"benchmarks/bundles/{bundle_file.name}"
