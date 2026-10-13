import json

from inferyard.application.types import CommandResult
from inferyard.cli import main


def test_overhead_cli_requires_explicit_budget_and_tolerance(tmp_path, capsys):
    seen = []

    def handler(request):
        seen.append(request)
        return 0, CommandResult(request.command, "passed")

    args = [
        "overhead",
        "--common-observer",
        "--plan",
        str(tmp_path / "plan.json"),
        "--trial",
        "t1",
        "--output-root",
        str(tmp_path / "out"),
        "--tolerance-ratio",
        ".05",
        "--max-wall-seconds",
        "60",
    ]
    assert main(args, handlers={"overhead": handler}) == 0
    assert seen[0].trial_id == "t1"
    assert seen[0].tolerance_ratio == 0.05
    assert seen[0].max_wall_seconds == 60
    assert seen[0].common_observer
    assert json.loads(capsys.readouterr().out)["command"] == "overhead"
    assert main(args[:-2], handlers={"overhead": handler}) == 2
    assert len(seen) == 1
    assert main([*args, "--boundary-observer"], handlers={"overhead": handler}) == 2
    boundary_args = [arg for arg in args if arg != "--common-observer"] + ["--boundary-observer"]
    assert main(boundary_args, handlers={"overhead": handler}) == 0
    assert seen[-1].boundary_observer and not seen[-1].common_observer
    assert (
        main(
            [*boundary_args, "--first-event-tolerance-ratio", ".03"], handlers={"overhead": handler}
        )
        == 0
    )
    assert seen[-1].first_event_tolerance_ratio == 0.03
    assert (
        main(
            [*boundary_args, "--engine-rate-tolerance-ratio", ".05"], handlers={"overhead": handler}
        )
        == 0
    )
    assert seen[-1].engine_rate_tolerance_ratio == 0.05
    assert (
        main([*boundary_args, "--block-gap-tolerance-ms", "2"], handlers={"overhead": handler}) == 0
    )
    assert seen[-1].block_gap_tolerance_ms == 2


def test_offline_command_routes_without_service(tmp_path, monkeypatch, capsys):
    import inferyard.application.overhead as cli

    monkeypatch.setattr(
        cli,
        "read_overhead",
        lambda root: {
            "passed": False,
            "status": "incomplete",
            "limitations": ["four_runs_required"],
        },
    )
    (tmp_path / "result.json").write_text(
        json.dumps(
            {
                "protocol_sha256": "a" * 64,
                "status": "incomplete",
                "passed": False,
                "limitations": [],
            }
        )
    )
    assert main(["verify", "--path", str(tmp_path)]) == 3
    result = json.loads(capsys.readouterr().out)
    assert result["completeness"] == "incomplete"
    assert result["details"]["passed"] is False


def test_engine_rate_refusal_is_not_hidden_by_e2e_pass(tmp_path, monkeypatch):
    import inferyard.application.overhead as cli

    monkeypatch.setattr(
        cli,
        "read_overhead",
        lambda root: {
            "passed": True,
            "status": "evaluated",
            "limitations": [],
            "engine_rate_assessments": {"L06": {"passed": True}, "L07": {"passed": False}},
        },
    )
    (tmp_path / "result.json").write_text(
        json.dumps(
            {
                "protocol_sha256": "a" * 64,
                "status": "incomplete",
                "passed": False,
                "limitations": [],
            }
        )
    )
    assert main(["verify", "--path", str(tmp_path)]) == 3


def test_block_p95_missing_is_not_hidden_by_other_statistics(tmp_path, monkeypatch):
    import inferyard.application.overhead as cli

    monkeypatch.setattr(
        cli,
        "read_overhead",
        lambda root: {
            "passed": True,
            "status": "evaluated",
            "limitations": [],
            "block_gap_assessments": {
                "within_request_p50": {"passed": True},
                "within_request_p95": {"passed": False},
            },
        },
    )
    (tmp_path / "result.json").write_text(
        json.dumps(
            {
                "protocol_sha256": "a" * 64,
                "status": "incomplete",
                "passed": False,
                "limitations": [],
            }
        )
    )
    assert main(["verify", "--path", str(tmp_path)]) == 3
