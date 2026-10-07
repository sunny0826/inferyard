"""Validate uv's runtime-only export against each target's locked dependency closure."""

import argparse
import json
import subprocess
import tomllib
from pathlib import Path

from packaging.markers import Marker, default_environment
from packaging.requirements import Requirement
from packaging.utils import canonicalize_name

ROOT = Path(__file__).resolve().parents[1]
TARGETS = {
    "linux-x64": ("linux", "Linux", "x86_64"),
    "windows-x64": ("win32", "Windows", "AMD64"),
    "macos-arm64": ("darwin", "Darwin", "arm64"),
}


def environments():
    for name, (system, platform, machine) in TARGETS.items():
        env = default_environment()
        env.update(
            sys_platform=system,
            platform_system=platform,
            platform_machine=machine,
            os_name="nt" if system == "win32" else "posix",
            python_version="3.14",
            python_full_version="3.14.7",
            implementation_name="cpython",
            extra="",
        )
        yield name, env


def active(marker, env):
    return not marker or Marker(marker).evaluate(env)


def closure(lock, env):
    packages = lock["package"]
    project = next(p for p in packages if p["name"] == "inferyard")
    pending, result = list(project["dependencies"]), {}
    while pending:
        dependency = pending.pop()
        if not active(dependency.get("marker"), env):
            continue
        name = canonicalize_name(dependency["name"])
        matches = [
            p
            for p in packages
            if canonicalize_name(p["name"]) == name
            and (not dependency.get("version") or p["version"] == dependency["version"])
            and (
                not p.get("resolution-markers")
                or any(active(m, env) for m in p["resolution-markers"])
            )
        ]
        if len(matches) != 1 or "registry" not in matches[0].get("source", {}):
            raise ValueError("ambiguous_or_non_registry_runtime_dependency")
        package = matches[0]
        if name in result:
            if result[name] != package["version"]:
                raise ValueError("conflicting_lock_versions")
            continue
        result[name] = package["version"]
        pending.extend(package.get("dependencies", []))
    return dict(sorted(result.items()))


def validate(text, lock, project):
    requirements = []
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        requirement = Requirement(line)
        specs = list(requirement.specifier)
        if (
            requirement.url
            or requirement.extras
            or len(specs) != 1
            or specs[0].operator != "=="
            or "*" in specs[0].version
        ):
            raise ValueError("constraints_require_exact_registry_versions")
        requirements.append(requirement)
    proof = {}
    used = set()
    for target, env in environments():
        exported = {}
        for index, requirement in enumerate(requirements):
            if requirement.marker and not requirement.marker.evaluate(env):
                continue
            name = canonicalize_name(requirement.name)
            if name in exported:
                raise ValueError("overlapping_constraints")
            exported[name] = next(iter(requirement.specifier)).version
            used.add(index)
        expected = closure(lock, env)
        if exported != expected:
            raise ValueError(f"runtime_closure_mismatch:{target}")
        for value in project["dependencies"]:
            requirement = Requirement(value)
            if not requirement.marker or requirement.marker.evaluate(env):
                if exported[canonicalize_name(requirement.name)] not in requirement.specifier:
                    raise ValueError("project_dependency_outside_declared_range")
        proof[target] = {"python": env["python_full_version"], "dependencies": expected}
    if len(used) != len(requirements):
        raise ValueError("unreachable_constraint")
    return proof


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--out", type=Path, required=True, help="new constraints file (and .validation.json)"
    )
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    lock = tomllib.loads((ROOT / "uv.lock").read_text(encoding="utf-8"))
    project = tomllib.loads((ROOT / "pyproject.toml").read_text(encoding="utf-8"))["project"]
    if args.check:
        text = args.out.read_text(encoding="utf-8")
    else:
        text = subprocess.run(
            [
                "uv",
                "export",
                "--frozen",
                "--no-dev",
                "--no-emit-project",
                "--no-hashes",
                "--format",
                "requirements-txt",
            ],
            cwd=ROOT,
            check=True,
            capture_output=True,
            text=True,
            encoding="utf-8",
        ).stdout
    proof = validate(text, lock, project)
    proof_path = args.out.with_suffix(args.out.suffix + ".validation.json")
    if not args.check:
        if (
            args.out.exists()
            or args.out.is_symlink()
            or proof_path.exists()
            or proof_path.is_symlink()
        ):
            raise FileExistsError("constraints_output_exists")
        with args.out.open("x", encoding="utf-8", newline="\n") as stream:
            stream.write(text)
        with proof_path.open("x", encoding="utf-8", newline="\n") as stream:
            json.dump(proof, stream, indent=2)
            stream.write("\n")
    elif json.loads(proof_path.read_text(encoding="utf-8")) != proof:
        raise ValueError("constraints_validation_drift")
    print(json.dumps({"checked": args.check, "targets": list(proof), "out": str(args.out)}))


if __name__ == "__main__":
    main()
