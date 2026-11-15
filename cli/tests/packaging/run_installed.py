"""Safe installed CLI matrix: offline commands only; no host lock, dirty state or model requests."""

import argparse
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

from jsonschema import Draft202012Validator

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "scripts"))
from release_common import (  # noqa: E402
    COMMANDS,
    EXIT_CODES,
    PROBES,
    SCOPE,
    STUDIO_SCHEMA,
    artifact_files,
    canonical_sha,
    read_json,
    sha,
    studio_report_schema,
    validate_installation,
)
from release_validation import validate_build  # noqa: E402

if __package__:
    from .fixture_wheel import make_upgrade_fixture, synthetic_version
else:
    from fixture_wheel import make_upgrade_fixture, synthetic_version


def environment(root):
    env = {
        k: v
        for k, v in os.environ.items()
        if not k.startswith(("UV_", "PIP_"))
        and k
        not in {"PYTHONPATH", "PYTHONHOME", "VIRTUAL_ENV", "CONDA_PREFIX", "CONDA_DEFAULT_ENV"}
    }
    env.update(
        {
            "UV_TOOL_DIR": str(root / "tools"),
            "UV_TOOL_BIN_DIR": str(root / "bin"),
            "UV_CACHE_DIR": str(root / "cache"),
            "UV_PYTHON_INSTALL_DIR": str(root / "python"),
            "UV_NO_CONFIG": "1",
            "PYTHONNOUSERSITE": "1",
            "PYTHONIOENCODING": "utf-8",
            "PIP_CONFIG_FILE": os.devnull,
            "XDG_CONFIG_HOME": str(root / "config"),
            "UV_PYTHON_DOWNLOADS": "automatic",
        }
    )
    return env


def validate_studio_document(report, out):
    """Bind the saved Studio document to the schema the installed CLI actually exports."""
    schema_raw = (out / "schema-studio-report.stdout").read_bytes()
    schema = read_json(schema_raw)
    if schema.get("$id") != studio_report_schema()["$id"]:
        raise ValueError("installed_studio_schema_identity_mismatch")
    raw = (report / "studio.json").read_bytes()
    Draft202012Validator(schema).validate(read_json(raw))
    return raw, schema_raw, schema


def run_matrix(wheel, constraints, inputs, out, manifest):
    build_raw = manifest.read_bytes()
    build = read_json(build_raw)
    files = artifact_files(manifest.parent, build)
    validate_build(build, files, build.get("source_commit", ""))
    if (
        wheel.read_bytes() != files["wheel"].raw
        or constraints.read_bytes() != files["constraints"].raw
    ):
        raise ValueError("installed_input_artifact_mismatch")
    out = out.resolve()
    checkout = Path(__file__).resolve().parents[2]
    if out.is_relative_to(checkout):
        raise ValueError("installed_test_requires_directory_outside_checkout")
    out.mkdir()
    incoming, cwd = out / "inputs", out / "empty-cwd"
    incoming.mkdir()
    cwd.mkdir()
    wheel_copy = incoming / wheel.name
    wheel_copy.write_bytes(files["wheel"].raw)
    constraint_copy = incoming / "runtime-constraints.txt"
    constraint_copy.write_bytes(files["constraints"].raw)
    shutil.copytree(inputs, incoming / "fixtures")
    shutil.copyfile(Path(__file__).with_name("installed_probe.py"), incoming / "probe.py")
    uv = Path(shutil.which("uv")).resolve()
    uvx = uv.with_name("uvx.exe" if os.name == "nt" else "uvx")
    env = environment(out)
    records = []
    base = ["--managed-python", "--python", "3.15.2"]

    def command(name, argv, *, expected=0, settings=None):
        result = subprocess.run(
            [str(a) for a in argv],
            cwd=cwd,
            env=settings or env,
            capture_output=True,
            text=True,
            timeout=600,
            encoding="utf-8",
        )
        (out / f"{name}.stdout").write_text(result.stdout, encoding="utf-8")
        (out / f"{name}.stderr").write_text(result.stderr, encoding="utf-8")
        records.append(
            {"name": name, "argv": [str(a) for a in argv], "returncode": result.returncode}
        )
        (out / "commands.json").write_text(json.dumps(records, indent=2), encoding="utf-8")
        if result.returncode != expected:
            raise RuntimeError(
                f"{name}: expected {expected}, got {result.returncode}: {result.stderr[-2000:]}"
            )
        return result.stdout

    command("install", [uv, "tool", "install", *base, "--constraints", constraint_copy, wheel_copy])
    cli = out / "bin" / ("inferyard.exe" if os.name == "nt" else "inferyard")
    python = out / "tools/inferyard" / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    # Once installed, all direct commands below run with uv's network disabled too.
    offline_env = dict(env, UV_OFFLINE="1", UV_PYTHON_DOWNLOADS="never")
    for name, args in [
        ("help", ["--help"]),
        ("versions", ["--versions"]),
        ("schema", ["--schema", "config"]),
        ("schema-studio-report", ["--schema", STUDIO_SCHEMA]),
        ("catalogue", ["catalogue", "--kind", "methods"]),
        ("engines", ["engine-fit", "engines"]),
        ("init", ["init", "--out", str(out / "中文 workspace")]),
    ]:
        command(name, [cli, *args], settings=offline_env)
    pinned = json.loads((out / "versions.stdout").read_text(encoding="utf-8"))
    target = {"linux": "linux-x64", "darwin": "macos-arm64", "win32": "windows-x64"}[sys.platform]
    proof = read_json(files["constraints_validation"].raw)
    for name, version in proof[target]["dependencies"].items():
        assert pinned["dependencies"][name] == version, (name, version, pinned)
    assert pinned["dependencies"]["pytest"] is None and pinned["dependencies"]["ruff"] is None
    assert pinned["python"] == "3.15.2"
    assert pinned["tool_version"] == build["version"]
    command(
        "probe-install-a",
        [
            python,
            "-I",
            incoming / "probe.py",
            "--expected",
            incoming / "fixtures/expected.json",
            "--fixtures",
            incoming / "fixtures",
            "--out",
            out / "formats-a",
        ],
        settings=offline_env,
    )
    fixture = json.loads((incoming / "fixtures/expected.json").read_text(encoding="utf-8"))[
        "fixture_relative"
    ]
    command(
        "report",
        [cli, "report", "--runs", incoming / "fixtures" / fixture, "--out", out / "report"],
        settings=offline_env,
    )
    studio_raw, schema_raw, schema = validate_studio_document(out / "report", out)
    verify = json.loads(
        command(
            "verify",
            [cli, "verify", "--path", out / "report"],
            expected=EXIT_CODES["verify"],
            settings=offline_env,
        )
    )
    if verify["limitations"] != ["studio_report_not_verifiable"]:
        raise ValueError("installed_verify_studio_limitations")
    retired = out / "retired-report"
    retired.mkdir()
    (retired / "index.json").write_text(
        json.dumps({"schema_version": 3, "report_format_version": 8}), encoding="utf-8"
    )
    legacy = json.loads(
        command(
            "verify-legacy-report",
            [cli, "verify", "--path", retired],
            expected=EXIT_CODES["verify-legacy-report"],
            settings=offline_env,
        )
    )
    if "unsupported_format" not in legacy["limitations"]:
        raise ValueError("installed_verify_retired_report")
    command(
        "tool-run",
        [
            uv,
            "tool",
            "run",
            "--isolated",
            *base,
            "--constraints",
            constraint_copy,
            "--from",
            wheel_copy,
            "inferyard",
            "--versions",
        ],
    )
    command(
        "uvx-hot-offline",
        [
            uvx,
            "--offline",
            "--isolated",
            *base,
            "--constraints",
            constraint_copy,
            "--from",
            wheel_copy,
            "inferyard",
            "--versions",
        ],
        settings=offline_env,
    )
    cold = dict(
        offline_env, UV_CACHE_DIR=str(out / "cold-cache"), UV_TOOL_DIR=str(out / "no-tools")
    )
    command(
        "uvx-cold-offline",
        [
            uvx,
            "--offline",
            "--isolated",
            *base,
            "--constraints",
            constraint_copy,
            "--from",
            wheel_copy,
            "inferyard",
            "--versions",
        ],
        expected=1,
        settings=cold,
    )
    command(
        "uvx-cold-online-rebuild",
        [
            uvx,
            "--isolated",
            *base,
            "--constraints",
            constraint_copy,
            "--from",
            wheel_copy,
            "inferyard",
            "--versions",
        ],
        settings=dict(cold, UV_OFFLINE="0", UV_PYTHON_DOWNLOADS="automatic"),
    )
    command(
        "install-different-dependency",
        [uv, "tool", "install", "--force", *base, "--with", "idna==3.10", wheel_copy],
    )
    different = json.loads(command("different-versions", [cli, "--versions"]))
    assert different["dependencies"]["idna"] == "3.10"
    isolated = json.loads(
        command(
            "uvx-isolates-existing-tool",
            [
                uvx,
                "--isolated",
                *base,
                "--constraints",
                constraint_copy,
                "--from",
                wheel_copy,
                "inferyard",
                "--versions",
            ],
        )
    )
    assert isolated["dependencies"] == pinned["dependencies"]
    assert json.loads(command("persistent-tool-unchanged", [cli, "--versions"])) == different
    # A second physical installation must preserve every resource and identity.
    second = dict(env, UV_TOOL_DIR=str(out / "tools-b"), UV_TOOL_BIN_DIR=str(out / "bin-b"))
    command(
        "install-b",
        [uv, "tool", "install", *base, "--constraints", constraint_copy, wheel_copy],
        settings=second,
    )
    python_b = (
        out / "tools-b/inferyard" / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    )
    command(
        "probe-install-b",
        [
            python_b,
            "-I",
            incoming / "probe.py",
            "--expected",
            incoming / "fixtures/expected.json",
            "--fixtures",
            incoming / "fixtures",
            "--out",
            out / "formats-b",
        ],
        settings=dict(second, UV_OFFLINE="1"),
    )
    before = hashlib.sha256((out / "中文 workspace/preparation.json").read_bytes()).hexdigest()
    upgrade = make_upgrade_fixture(wheel_copy, incoming)
    command(
        "synthetic-upgrade",
        [uv, "tool", "install", "--force", *base, "--constraints", constraint_copy, upgrade],
    )
    upgraded = json.loads(command("synthetic-upgrade-versions", [cli, "--versions"]))
    assert upgraded["tool_version"] == synthetic_version(build["version"])
    command(
        "rollback",
        [uv, "tool", "install", "--force", *base, "--constraints", constraint_copy, wheel_copy],
    )
    assert json.loads(command("rollback-versions", [cli, "--versions"])) == pinned
    command(
        "probe-rollback",
        [
            python,
            "-I",
            incoming / "probe.py",
            "--expected",
            incoming / "fixtures/expected.json",
            "--fixtures",
            incoming / "fixtures",
            "--out",
            out / "formats-rollback",
        ],
        settings=offline_env,
    )
    assert (
        hashlib.sha256((out / "中文 workspace/preparation.json").read_bytes()).hexdigest() == before
    )
    probe_checks = {}
    for name in PROBES:
        raw = (out / f"{name}.stdout").read_bytes()
        probe = read_json(raw)
        probe_checks[name] = {
            "stdout_sha256": sha(raw),
            "current_studio_report_kind": probe["current_studio_report_kind"],
            "unsupported_report_formats_rejected": probe[
                "unsupported_report_formats_rejected"
            ],
            "resource_files": probe["resource_files"],
            "tool_source_hash": probe["tool_source_hash"],
            "scorer_hash": probe["scorer_hash"],
        }
    if [record["name"] for record in records] != list(COMMANDS):
        raise ValueError("installed_command_order")
    result = {
        "kind": "installed_safe_checks.v4",
        "status": "passed",
        "completed": True,
        "source_commit": build["source_commit"],
        "build_manifest_sha256": sha(build_raw),
        "artifact_sha256": {role: file.digest for role, file in files.items()},
        "version": build["version"],
        "scope": SCOPE,
        "checks": [{"name": r["name"], "returncode": r["returncode"]} for r in records],
        "wheel_sha256": files["wheel"].digest,
        "studio_report_schema": {
            "$id": schema["$id"],
            "argv": [str(cli), "--schema", STUDIO_SCHEMA],
            "stdout_sha256": sha(schema_raw),
            "canonical_json_sha256": canonical_sha(schema),
        },
        "studio_json": {
            "path": "report/studio.json",
            "sha256": sha(studio_raw),
            "schema_validated": True,
            "verify_exit_code": next(
                r["returncode"] for r in reversed(records) if r["name"] == "verify"
            ),
            "verify_limitations": verify["limitations"],
        },
        "probe_checks": probe_checks,
        "commands": len(records),
        "model_requests_sent": 0,
        "host_lock_tests": "not_run",
        "platform": sys.platform,
        "architecture": platform.machine(),
        "python": pinned["python"],
        "limitations": [
            "not_clean_os",
            "native_engine_prepare_not_run",
            "upgrade_uses_synthetic_next_development_wheel_not_a_release",
            "network_not_os_sandboxed",
        ],
    }
    validate_installation(result, build, sha(build_raw), files)
    (out / "result.json").write_text(json.dumps(result, indent=2), encoding="utf-8")
    print(json.dumps(result))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--wheel", type=Path, required=True)
    parser.add_argument("--constraints", type=Path, required=True)
    parser.add_argument("--inputs", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    run_matrix(
        args.wheel.resolve(),
        args.constraints.resolve(),
        args.inputs.resolve(),
        args.out,
        args.manifest.resolve(),
    )


if __name__ == "__main__":
    main()
