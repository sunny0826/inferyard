"""Run local-only real macOS window UI E2E against existing local PolyStrata assets (not in CI)."""

from __future__ import annotations

import argparse
import json
import os
import plistlib
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path

from run import require_unlocked_desktop, resolve_derived_data

PASS_MARKER = (
    "Test Case '-[PreviewUITests.PolystrataRealUITests testRealPolystrataStudioLifecycle]' passed"
)
WEIGHT_FLAGS = frozenset({"--pack", "--native", "--ple-gguf", "--model", "--mtp"})


@dataclass(frozen=True)
class StagedPolystrata:
    installation_path: Path
    work_dir: Path
    data_dir: Path
    model_name: str
    src_work_dir: Path
    src_data_dir: Path
    work_snapshot: dict[str, str]
    data_snapshot: dict[str, str]


def default_cli_path() -> Path:
    repo_root = Path(__file__).resolve().parents[3]
    return repo_root / "cli/.venv/bin/inferyard"


def resolve_local_installation(override: Path | None) -> Path:
    if override is not None:
        resolved = override.expanduser().resolve()
        if not resolved.is_file():
            raise FileNotFoundError(f"PolyStrata installation.json is missing: {resolved}")
        return resolved
    env_override = os.environ.get("INFERYARD_POLYSTRATA_INSTALLATION", "").strip()
    if env_override:
        resolved = Path(env_override).expanduser().resolve()
        if not resolved.is_file():
            raise FileNotFoundError(
                f"INFERYARD_POLYSTRATA_INSTALLATION is missing: {resolved}"
            )
        return resolved
    home = Path.home()
    candidates = [
        home / ".inferyard/engines/polystrata/installation.json",
        home / "PolyStrata/installation.json",
        home / "polystrata/installation.json",
    ]
    for candidate in candidates:
        if candidate.is_file():
            return candidate
    raise FileNotFoundError(
        f"no local PolyStrata installation.json found in {[str(c) for c in candidates]} "
        "(downloading models is forbidden; pass --installation or INFERYARD_POLYSTRATA_INSTALLATION)"
    )


def snapshot_dir_state(root: Path) -> dict[str, str]:
    snap: dict[str, str] = {}
    for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
        current = Path(dirpath)
        rel_dir = current.relative_to(root)
        kept_dirs: list[str] = []
        for dirname in sorted(dirnames):
            child = current / dirname
            rel = str(rel_dir / dirname) if rel_dir != Path(".") else dirname
            if child.is_symlink():
                snap[rel] = f"sym:{os.readlink(child)}"
            elif dirname == ".venv":
                snap[rel] = "dir:.venv"
            else:
                snap[rel] = "dir"
                kept_dirs.append(dirname)
        dirnames[:] = kept_dirs
        for filename in sorted(filenames):
            child = current / filename
            rel = str(rel_dir / filename) if rel_dir != Path(".") else filename
            if child.is_symlink():
                snap[rel] = f"sym:{os.readlink(child)}"
            else:
                st = child.lstat()
                snap[rel] = f"file:{st.st_size}:{st.st_mtime_ns}"
    return snap


def verify_dir_unchanged(label: str, root: Path, expected: dict[str, str]) -> None:
    actual = snapshot_dir_state(root)
    if actual != expected:
        raise RuntimeError(f"{label} was mutated during UI E2E: {root}")


def clone_tree_cow(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    if sys.platform == "darwin":
        cp_cow = subprocess.run(
            ["/bin/cp", "-cR", str(src), str(dst)], capture_output=True, check=False
        )
        if cp_cow.returncode == 0:
            return
    subprocess.run(["cp", "-R", str(src), str(dst)], check=True)


def rewrite_prefixes(path: Path, replacements: list[tuple[str, str]]) -> None:
    raw = path.read_text(encoding="utf-8")
    updated = raw
    for old, new in replacements:
        if old and old != new:
            updated = updated.replace(old, new)
    if updated != raw:
        path.write_text(updated, encoding="utf-8")


def estimate_strata_disk_bytes(cfg_path: Path) -> int:
    try:
        doc = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return 1 << 60
    args = doc.get("args")
    if not isinstance(args, list):
        return 1 << 60
    total = 0
    for idx in range(len(args) - 1):
        if args[idx] in WEIGHT_FLAGS and isinstance(args[idx + 1], str):
            target = Path(args[idx + 1])
            if target.is_file():
                total += target.stat().st_size
            elif target.is_dir():
                for root, _, files in os.walk(target):
                    for name in files:
                        fp = Path(root) / name
                        if fp.is_file() and not fp.is_symlink():
                            total += fp.stat().st_size
    return total if total > 0 else (1 << 60)


def polystrata_config_display_name(cfg_path: Path) -> str:
    try:
        doc = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        doc = {}
    if isinstance(doc, dict):
        for key in ("display_name", "model_name"):
            raw = doc.get(key)
            if isinstance(raw, str):
                cleaned = " ".join(raw.split())
                if cleaned:
                    return cleaned
    return cfg_path.stem or cfg_path.name


def stage_local_polystrata(src_inst_path: Path, workspace: Path) -> StagedPolystrata:
    decl = json.loads(src_inst_path.read_text(encoding="utf-8"))
    if decl.get("family") != "polystrata":
        raise ValueError(f"expected family=polystrata in {src_inst_path}")
    src_work = Path(str(decl.get("working_directory") or src_inst_path.parent)).expanduser()
    raw_data = str(decl.get("data_dir") or "").strip()
    if not raw_data:
        raise ValueError(f"missing data_dir in {src_inst_path}")
    src_data = Path(raw_data).expanduser()

    work_snap = snapshot_dir_state(src_work)
    data_snap = snapshot_dir_state(src_data)

    dst_work = workspace / "engines/polystrata"
    dst_data = workspace / "models/polystrata-data"
    clone_tree_cow(src_work, dst_work)
    clone_tree_cow(src_data, dst_data)

    replacements = [
        (str(src_work), str(dst_work)),
        (str(src_work.resolve()), str(dst_work)),
        (str(src_data), str(dst_data)),
        (str(src_data.resolve()), str(dst_data)),
    ]
    dst_inst = dst_work / "installation.json"
    rewrite_prefixes(dst_inst, replacements)

    configs: list[tuple[int, str, Path]] = []
    for search_dir in (dst_work, dst_data, dst_data / "models"):
        if not search_dir.is_dir():
            continue
        for entry in sorted(search_dir.iterdir()):
            if not entry.is_file():
                continue
            if entry.suffix == ".log":
                entry.unlink(missing_ok=True)
                continue
            if entry.name.startswith("strata-") and entry.name.endswith(".json"):
                rewrite_prefixes(entry, replacements)
                configs.append((estimate_strata_disk_bytes(entry), str(entry), entry))
    if not configs:
        raise RuntimeError(f"no strata-*.json found in staged PolyStrata ({dst_work}, {dst_data})")
    configs.sort(key=lambda item: (item[0], item[1]))
    chosen = configs[0][2]
    for _, _, extra in configs[1:]:
        extra.unlink(missing_ok=True)
    model_name = polystrata_config_display_name(chosen)
    return StagedPolystrata(
        installation_path=dst_inst,
        work_dir=dst_work,
        data_dir=dst_data,
        model_name=model_name,
        src_work_dir=src_work,
        src_data_dir=src_data,
        work_snapshot=work_snap,
        data_snapshot=data_snap,
    )


def build_local_studio_app(studio_dir: Path, output: Path) -> Path:
    app = output / "app/InferYard Studio.app"
    macos_dir = app / "Contents/MacOS"
    macos_dir.mkdir(parents=True, exist_ok=True)
    binary = macos_dir / "InferYard Studio"
    env = dict(
        os.environ,
        GOTOOLCHAIN="go1.27.3",
        GOPROXY=os.environ.get("GOPROXY", "https://goproxy.cn,direct"),
        CGO_ENABLED="0",
    )
    subprocess.run(["go", "build", "-o", str(binary), "."], cwd=studio_dir, env=env, check=True)
    info = {
        "CFBundleDevelopmentRegion": "en",
        "CFBundleDisplayName": "InferYard Studio",
        "CFBundleExecutable": "InferYard Studio",
        "CFBundleIdentifier": "local.inferyard.studio-polystrata-ui",
        "CFBundleInfoDictionaryVersion": "6.0",
        "CFBundleLocalizations": ["en", "zh-Hans"],
        "CFBundleName": "InferYard Studio",
        "CFBundlePackageType": "APPL",
        "CFBundleShortVersionString": "0.0.0-local",
        "CFBundleVersion": "1",
        "LSMinimumSystemVersion": "14.0",
        "NSHighResolutionCapable": True,
        "NSPrincipalClass": "NSApplication",
    }
    (app / "Contents/Info.plist").write_bytes(plistlib.dumps(info))
    return app


def prepare_environment(
    output: Path, cli: Path, installation_path: Path
) -> tuple[Path, Path, Path, StagedPolystrata]:
    host_dir = output / "host"
    host_dir.mkdir(parents=True, exist_ok=True)
    bootstrap = Path(__file__).resolve().parents[1] / "e2e/cli_bootstrap.py"
    shutil.copyfile(bootstrap, host_dir / "sitecustomize.py")
    existing_py = os.environ.get("PYTHONPATH", "").strip()
    python_path = f"{host_dir}{os.pathsep}{existing_py}" if existing_py else str(host_dir)

    workspace = output / "workspace"
    cli_env = dict(os.environ, PYTHONPATH=python_path)
    subprocess.run([str(cli), "init", "--out", str(workspace)], env=cli_env, check=True)
    staged = stage_local_polystrata(installation_path, workspace)

    home = output / "home"
    settings_dir = home / "Library/Application Support/InferYard Studio"
    settings_dir.mkdir(parents=True, exist_ok=True)
    settings_dir.chmod(0o700)
    settings = {
        "inferyard_path": str(cli),
        "workspace_root": str(workspace),
        "keep_services_on_quit": False,
        "engine_installations": [
            {
                "path": str(staged.installation_path),
                "enabled": True,
            }
        ],
    }
    settings_path = settings_dir / "settings.json"
    settings_path.write_text(json.dumps(settings, indent=2) + "\n", encoding="utf-8")
    settings_path.chmod(0o600)
    return home, workspace, Path(python_path), staged


def xcode_command(output: Path, derived_data: Path) -> list[str]:
    return [
        "xcodebuild",
        "test",
        "-project",
        str(Path(__file__).parent / "PreviewUI.xcodeproj"),
        "-scheme",
        "PreviewUI",
        "-destination",
        "platform=macOS",
        "-derivedDataPath",
        str(derived_data),
        "-resultBundlePath",
        str(output / "Results.xcresult"),
        "-parallel-testing-enabled",
        "NO",
        "-only-testing:PreviewUITests/PolystrataRealUITests",
        "-test-timeouts-enabled",
        "YES",
        "-default-test-execution-time-allowance",
        "1800",
        "-maximum-test-execution-time-allowance",
        "1800",
    ]


def terminate_workspace_processes(workspace: Path) -> None:
    needle = str(workspace)
    proc = subprocess.run(["ps", "-ax", "-o", "pid=,command="], capture_output=True, text=True, check=False)
    for line in proc.stdout.splitlines():
        parts = line.strip().split(None, 1)
        if len(parts) == 2 and needle in parts[1]:
            try:
                pid = int(parts[0])
                if pid != os.getpid():
                    os.kill(pid, signal.SIGTERM)
            except (ValueError, ProcessLookupError, PermissionError):
                pass


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, default=None, help="prebuilt InferYard Studio.app")
    parser.add_argument("--cli", type=Path, default=None, help="inferyard CLI executable")
    parser.add_argument("--installation", type=Path, default=None, help="local PolyStrata installation.json")
    parser.add_argument("--output-root", type=Path, default=Path("build/polystrata-ui-e2e"))
    parser.add_argument("--derived-data", type=Path, default=None)
    parser.add_argument("--keep-workspace", action="store_true")
    args = parser.parse_args()

    if sys.platform != "darwin":
        parser.error("local PolyStrata native UI E2E requires macOS, an unlocked desktop, and Xcode")
    cli = (args.cli or default_cli_path()).expanduser().resolve()
    if not cli.is_file():
        parser.error(f"CLI executable is missing: {cli} (run `mise run cli:sync` first)")
    try:
        installation = resolve_local_installation(args.installation)
    except FileNotFoundError as exc:
        parser.error(str(exc))
    try:
        require_unlocked_desktop()
    except (OSError, TypeError, ValueError, plistlib.InvalidFileException, subprocess.SubprocessError) as exc:
        parser.error(f"desktop preflight failed: {exc}")

    args.output_root.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="run-", dir=args.output_root.resolve()))
    derived_data = resolve_derived_data(args.output_root, args.derived_data)
    derived_data.mkdir(parents=True, exist_ok=True)
    studio_dir = Path(__file__).resolve().parents[2]
    app = args.app.expanduser().resolve() if args.app else build_local_studio_app(studio_dir, output)
    if not app.is_dir():
        parser.error(f"Studio .app bundle is missing: {app}")

    home, workspace, python_path, staged = prepare_environment(output, cli, installation)
    log = output / "xcodebuild.log"
    exit_code = 1
    try:
        print(f"PolyStrata real UI E2E artifacts: {output}", flush=True)
        print(f"Staged smallest local model: {staged.model_name}", flush=True)
        env = dict(
            os.environ,
            TEST_RUNNER_INFERYARD_POLYSTRATA_UI_APP=str(app),
            TEST_RUNNER_INFERYARD_POLYSTRATA_UI_HOME=str(home),
            TEST_RUNNER_INFERYARD_POLYSTRATA_UI_PYTHONPATH=str(python_path),
            TEST_RUNNER_INFERYARD_POLYSTRATA_UI_MODEL=staged.model_name,
        )
        with log.open("w", encoding="utf-8") as stream:
            started = time.perf_counter()
            result = subprocess.run(
                xcode_command(output, derived_data),
                stdout=stream,
                stderr=subprocess.STDOUT,
                check=False,
                env=env,
            )
            elapsed = time.perf_counter() - started
        log_text = log.read_text(encoding="utf-8", errors="replace")
        exit_code = result.returncode
        if exit_code == 0 and PASS_MARKER not in log_text:
            exit_code = 65
            print("PolystrataRealUITests did not report a passing result", flush=True)
        reports = sorted(workspace.glob("reports/*/studio.json"))
        copied_report = ""
        if exit_code == 0:
            if not reports:
                exit_code = 66
                print("PolystrataRealUITests produced no studio.json report", flush=True)
            else:
                report_doc = json.loads(reports[-1].read_text(encoding="utf-8"))
                if report_doc.get("schema_version") != 3:
                    exit_code = 66
                    print("Generated studio.json did not match schema_version=3", flush=True)
                else:
                    dst_report = output / "studio.json"
                    shutil.copyfile(reports[-1], dst_report)
                    copied_report = str(dst_report)
        manifest = {
            "suite": "polystrata-real",
            "model": staged.model_name,
            "elapsed_seconds": elapsed,
            "exit_code": exit_code,
            "app": str(app),
            "report": copied_report,
        }
        (output / "run.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        status = "passed" if exit_code == 0 else "failed"
        print(f"PolyStrata real UI E2E {status}; build and test: {elapsed:.1f}s", flush=True)
        if exit_code:
            if "Timed out while enabling automation mode" in log_text:
                print(
                    "macOS UI Automation authorization timed out: approve the desktop password "
                    "prompt or run `sudo automationmodetool "
                    "enable-automationmode-without-authentication`.",
                    flush=True,
                )
            lines = log_text.splitlines()
            failures = [line for line in lines[:-200] if ": error:" in line]
            print("\n".join([*failures, *lines[-200:]]))
    finally:
        terminate_workspace_processes(workspace)
        if not args.keep_workspace:
            shutil.rmtree(workspace, ignore_errors=True)
        verify_dir_unchanged(
            "host PolyStrata working_directory", staged.src_work_dir, staged.work_snapshot
        )
        verify_dir_unchanged("host PolyStrata data_dir", staged.src_data_dir, staged.data_snapshot)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
