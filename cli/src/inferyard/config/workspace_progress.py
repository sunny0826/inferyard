"""Discover onboarding artifacts; reuse authoritative offline evidence readers."""

import shlex
from pathlib import Path

from inferyard.config.community_resources import BUNDLES
from inferyard.config.preparation_io import PreparationError, read_json
from inferyard.config.workspace_inputs import latest, latest_preflight, preflight_files


def command(*parts):
    return shlex.join(["inferyard", *(str(part) for part in parts)])


def new_output(root, stem):
    number = 1
    while (root / f"{stem}-{number}").exists() or (root / f"{stem}-{number}").is_symlink():
        number += 1
    return root / f"{stem}-{number}"


def preparation(path, kind):
    value = read_json(path)
    if type(value) is not dict or value.get("kind") != kind or value.get("schema_version") != 3:
        raise PreparationError("workspace_artifact_unreadable")
    return value


def artifact_paths(root, marker):
    if not root.exists():
        return []
    if not root.is_dir():
        raise PreparationError("workspace_artifact_unreadable")
    return [path / marker for path in root.iterdir() if path.is_dir() and (path / marker).exists()]


def candidates(workspace):
    records = []
    for path in artifact_paths(workspace / "candidates", "preparation.json"):
        record = preparation(path, "config_candidate.v1")
        candidate = Path(record["candidate"])
        candidate.read_bytes()
        records.append((path.parent, record))
    return records


def bindings(workspace):
    records = []
    for path in artifact_paths(workspace / "bound", "preparation.json"):
        record = preparation(path, "config_binding.v1")
        Path(record["config"]).read_bytes()
        records.append((path.parent, record))
    return records


def run_records(workspace):
    from inferyard.evidence.ledger import read_trial

    root = workspace / "results"
    if not root.exists():
        return []
    if not root.is_dir():
        raise PreparationError("workspace_artifact_unreadable")
    paths = []
    for child in root.iterdir():
        if not child.is_dir():
            continue
        if (child / "runs").is_dir():
            if (child / "batch.json").exists():
                read_json(child / "batch.json")
            paths.extend(p for p in (child / "runs").iterdir() if p.is_dir())
        elif (child / "inputs.json").exists() or (child / "run.json").exists():
            paths.append(child)
    records = []
    for path in sorted(paths):
        metadata = {}
        data = read_trial(path, metadata=metadata)
        records.append(
            (
                path,
                data,
                {
                    "path": str(path.resolve()),
                    "complete": data["summary"]["completeness"] == "complete",
                    "sealed": metadata["manifest"] is not None,
                },
            )
        )
    return records


def report_state(workspace, completed):
    from inferyard.evidence.source_locations import resolve_source
    from inferyard.reporting.report import verify_report

    matching = []
    for marker in artifact_paths(workspace / "reports", "index.json"):
        saved = read_json(marker)
        (marker.parent / "report.html").read_bytes()
        status = "present"
        if (marker.parent / "artifact-manifest.json").exists():
            if verify_report(marker.parent).get("verified") is True:
                status = "verified"
        paths = [str(resolve_source(marker.parent, run["source"]["path"])) for run in saved["runs"]]
        if set(completed).issubset(paths) and completed:
            matching.append((marker.parent, paths, status))
    if not matching:
        return {"status": "missing", "path": None, "runs": []}
    path = latest([path for path, _, _ in matching])
    paths, status = next(
        (paths, status) for candidate, paths, status in matching if candidate == path
    )
    return {"status": status, "path": str(path), "runs": paths}


def run_groups(matching):
    """Group a bundle's runs by frozen plan, newest group last (run ids sort by time)."""
    groups = {}
    for entry in matching:
        groups.setdefault(entry[1]["run"]["plan_sha256"], []).append(entry)
    return sorted(groups.values(), key=lambda group: group[-1][0].name)


def group_complete(group):
    """Reuse the authoritative plan history projection, not a per-run shortcut."""
    from inferyard.evidence.ledger import plan_progress

    plan = group[0][1]["plan"]
    progress = plan_progress(plan, [data for _, data, _ in group])
    return all(trial["status"] == "complete" for trial in progress)


def unfinished_attempt(group):
    """Latest attempt of the first incomplete trial, mirroring the batch runner's choice."""
    by_trial = {}
    for path, data, _ in group:
        by_trial.setdefault(data["run"]["trial_id"], []).append((path, data))
    for trial in group[0][1]["plan"]["trials"]:
        attempts = by_trial.get(trial["trial_id"], ())
        if not attempts:
            return None
        path, data = attempts[-1]
        if data["summary"]["completeness"] != "complete":
            return path, data
    return None


def resume_command(path, data, bound):
    """resume for saved unexecuted cases; otherwise restart via the plan or the bound config."""
    batch = path.parent.parent
    if data["run"]["execution_mode"] == "single":
        return (
            "run",
            command("run", "--config", bound["config"]),
            "single_run_resume_not_supported",
        )
    if any(
        workload["protocol"]["kind"] == "duration"
        for workload in data["plan"]["experiment"]["workloads"]
    ):
        return (
            "run",
            command(
                "run",
                "--plan",
                batch / "frozen-plan" / "plan.json",
                "--output-root",
                batch,
            ),
            "duration_resume_requires_new_window",
        )
    if any(request["execution_state"] == "not_executed" for request in data["requests"]):
        return "resume", command("resume", "--from-run", path), None
    return (
        "run",
        command("run", "--plan", batch / "frozen-plan" / "plan.json", "--output-root", batch),
        "batch_plan_not_finished",
    )


def bundle_state(workspace, name, bundle_path, preflight, candidate_records, bound_records, runs):
    bundle = read_json(bundle_path)
    candidate_paths = {
        path: record
        for path, record in candidate_records
        if Path(record["bundle"]).resolve() == bundle_path
    }
    candidate_path = latest(candidate_paths)
    candidate = candidate_paths.get(candidate_path)
    bound_paths = {
        path: record
        for path, record in bound_records
        if candidate and Path(record["candidate"]).resolve() == Path(candidate["candidate"])
    }
    bound_path = latest(bound_paths)
    bound = bound_paths.get(bound_path)
    matching = [
        (path, data, state)
        for path, data, state in runs
        if data["bundle"] == bundle and not data["run"]["diagnostic"]
    ]
    states = [state for _, _, state in matching]
    report_runs = None
    result = {
        "bundle": name,
        "phase": "preflight_missing",
        "candidate_path": str(candidate_path) if candidate_path else None,
        "bound_path": str(bound_path) if bound_path else None,
        "runs": states,
        "next_action": "device_check",
        "next_command": None,
        "blocked_reason": None,
    }
    if preflight is None:
        result["next_command"] = device_command(workspace)
    elif candidate is None:
        result.update(phase="candidate_missing", next_action="prepare_candidate")
        report = read_json(preflight)
        system = report["hardware"]["platform"]
        option = "--runtime-receipt" if system == "Windows" else "--engine"
        variable = "INFERYARD_RUNTIME_RECEIPT" if system == "Windows" else "INFERYARD_ENGINE"
        result["next_command"] = (
            command("config", "create", "--workspace", workspace, "--bundle", name)
            + f' {option} "${{{variable}:?Set {variable} to the existing local asset}}" '
            + shlex.join(["--out", str(new_output(workspace / "candidates", name))])
        )
        recommendation = report.get("recommendation", {})
        if recommendation.get("status") != "recommended":
            result["blocked_reason"] = recommendation.get("reason") or "device_preflight_blocked"
        if system not in ("Darwin", "Windows"):
            result["blocked_reason"] = "unsupported_platform"
    elif bound is None:
        result.update(phase="bound_missing", next_action="bind")
        result["next_command"] = (
            command("config", "bind", "--candidate", candidate["candidate"])
            + ' --pid "${INFERYARD_SERVER_PID:?Set the externally started service PID}"'
            + ' --endpoint "${INFERYARD_ENDPOINT:?Set the local service endpoint}" '
            + shlex.join(["--out", str(new_output(workspace / "bound", name))])
        )
    elif not matching:
        result.update(
            phase="run_missing",
            next_action="run",
            next_command=command("run", "--config", bound["config"]),
        )
    else:
        groups = run_groups(matching)
        if group_complete(groups[-1]):
            result.update(phase="run_complete", next_action="none")
            report_runs = [state["path"] for _, data, state in groups[-1] if state["complete"]]
        else:
            attempt = unfinished_attempt(groups[-1]) or groups[-1][-1][:2]
            action, text, blocked = resume_command(attempt[0], attempt[1], bound)
            result.update(phase="run_incomplete", next_action=action, next_command=text)
            if blocked:
                result["blocked_reason"] = blocked
    if name == "zh-context-speed":
        from inferyard.config.context_speed_progress import update

        return update(workspace, result, bundle, bound)
    return result, report_runs


def device_command(workspace):
    return command("device-check", "--json", "--out", new_output(workspace / "preflight", "device"))


def inspect_workspace(path):
    workspace = path.resolve(strict=True)
    initialized = preparation(workspace / "preparation.json", "community_init.v1")
    for preflight in preflight_files(workspace):
        report = read_json(preflight)
        if (
            type(report) is not dict
            or report.get("kind") != "device_preflight.v1"
            or type(report.get("hardware")) is not dict
            or type(report.get("recommendation")) is not dict
        ):
            raise PreparationError("workspace_artifact_unreadable")
    preflight = latest_preflight(workspace)
    candidate_records, bound_records = candidates(workspace), bindings(workspace)
    runs = run_records(workspace)
    names = initialized["bundles"]
    if (
        type(names) is not list
        or not names
        or any(type(item) is not dict or item.get("bundle") not in BUNDLES for item in names)
        or len({item["bundle"] for item in names}) != len(names)
    ):
        raise PreparationError("workspace_artifact_unreadable")
    names = sorted(names, key=lambda item: (item["bundle"] != "zh-core", item["bundle"]))
    bundles = []
    report_sources = []
    for item in names:
        name = item["bundle"]
        bundle_path = (workspace / "bundles" / (name + ".json")).resolve(strict=True)
        state, report_runs = bundle_state(
            workspace, name, bundle_path, preflight, candidate_records, bound_records, runs
        )
        bundles.append(state)
        report_sources.extend(report_runs or ())
    completed = report_sources if all(b["phase"] == "run_complete" for b in bundles) else []
    report = report_state(workspace, completed)
    pending = next((bundle for bundle in bundles if bundle["next_action"] != "none"), None)
    if pending:
        action = "run" if pending["next_action"] == "resume" else pending["next_action"]
        next_command = pending["next_command"]
    elif report["status"] == "missing":
        action = "report"
        next_command = command(
            "report", "--runs", *completed, "--out", new_output(workspace / "reports", "summary")
        )
    elif report["status"] == "present":
        action, next_command = "verify", command("verify", "--path", report["path"])
    else:
        action, next_command = "done", None
    return {
        "kind": "progress",
        "progress_version": 1,
        "workspace": str(workspace),
        "device_preflight": {
            "status": "ok" if preflight else "missing",
            "path": str(preflight) if preflight else None,
        },
        "bundles": bundles,
        "report": report,
        "next_action": action,
        "next_command": next_command,
        "default_scope_complete": bool(bundles) and action == "done",
    }
