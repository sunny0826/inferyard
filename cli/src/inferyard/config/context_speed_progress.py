"""Read-only next actions for the dedicated five-trial workflow."""

from inferyard.config.context_speed_profile import classify_verified_inputs
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.workspace_inputs import latest
from inferyard.evidence.context_speed import read_inputs


def update(workspace, result, bundle, bound):
    from inferyard.config.workspace_progress import command, new_output

    batches = []
    results = workspace / "results"
    if results.is_dir():
        for path in results.iterdir():
            if not path.is_dir() or not (path / "batch.json").exists():
                continue
            plan, loaded = read_frozen_plan(path / "frozen-plan/plan.json")
            if not any(
                classify_verified_inputs(plan, item.config.to_dict(), item.bundle.to_dict())
                for item in loaded.values()
            ):
                continue
            _, loaded, _ = read_inputs(path)
            if any(item.bundle.to_dict() == bundle for item in loaded.values()):
                batches.append(path)
    batch = latest(batches)
    if batch is not None:
        result.update(
            phase="results_available",
            next_action="speed_results",
            next_command=command("speed-results", "--batch", batch),
            blocked_reason=None,
        )
    elif bound:
        plans = []
        if results.is_dir():
            for path in results.iterdir():
                if not path.is_dir() or not (path / "plan.json").exists():
                    continue
                plan, loaded = read_frozen_plan(path / "plan.json")
                if all(
                    classify_verified_inputs(plan, item.config.to_dict(), item.bundle.to_dict())
                    and item.bundle.to_dict() == bundle
                    for item in loaded.values()
                ):
                    plans.append(path / "plan.json")
        frozen = latest(plans)
        if frozen:
            result.update(
                phase="run_missing",
                next_action="run",
                blocked_reason=None,
                next_command=command(
                    "run", "--plan", frozen, "--output-root", new_output(results, "speed-batch")
                ),
            )
            return result, None
        result.update(
            phase="plan_missing",
            next_action="freeze_plan",
            blocked_reason=None,
            next_command=command(
                "plan",
                "--preset",
                "context-speed",
                "--config",
                bound["config"],
                "--out",
                new_output(workspace / "results", "speed-plan"),
            ),
        )
    elif result["next_action"] == "prepare_candidate":
        result["next_command"] += (
            " --profile context-speed --context-size "
            '"${INFERYARD_CONTEXT_SIZE:?Choose 65536, 131072 or 262144}"'
        )
    return result, None
