"""Lightweight CLI for same-host, same-model engine diagnostics."""

from pathlib import Path

from inferyard.config.engine_fit import ENGINES


def add_commands(commands):
    group = commands.add_parser(
        "engine-fit", help="plan, run and compare local inference engine diagnostics"
    )
    actions = group.add_subparsers(dest="fit_action", required=True)
    engines = actions.add_parser(
        "engines", help="list engine capabilities and requirements offline"
    )
    engines.set_defaults(command="engine-fit engines")
    plan = actions.add_parser("plan", help="freeze a local model directory or single GGUF")
    plan.set_defaults(command="engine-fit plan")
    plan.add_argument("--model", dest="model_path", type=Path, required=True)
    plan.add_argument(
        "--engines",
        dest="fit_engines",
        nargs="+",
        choices=ENGINES,
    )
    plan.add_argument("--prompts", dest="fit_prompts", type=Path)
    plan.add_argument("--max-tokens", dest="fit_max_tokens", type=int, default=128)
    plan.add_argument("--request-timeout", dest="fit_timeout", type=float, default=60.0)
    plan.add_argument("--repetitions", dest="fit_repetitions", type=int, default=1)
    memory = plan.add_mutually_exclusive_group()
    memory.add_argument("--min-free-memory-mib", dest="fit_min_memory_mib", type=int, default=512)
    memory.add_argument(
        "--skip-memory-stop",
        dest="fit_memory_stop_override_reason",
        metavar="REASON",
        help="freeze a reason to disable memory stopping; still collect memory and missing reasons",
    )
    plan.add_argument(
        "--skip-temperature-stop",
        dest="fit_temperature_stop_override_reason",
        metavar="REASON",
        help="freeze an explicit reason to disable temperature stopping; still collect temperature",
    )
    plan.add_argument("--out", type=Path, required=True)
    run = actions.add_parser("run", help="diagnose an externally started Linux/macOS service")
    run.set_defaults(command="engine-fit run")
    run.add_argument("--plan", dest="frozen_plan", type=Path, required=True)
    run.add_argument("--engine", dest="fit_engine", choices=ENGINES, required=True)
    run.add_argument("--endpoint-url", required=True)
    run.add_argument("--server-pid", type=int, required=True)
    run.add_argument("--served-model", dest="fit_served_model", required=True)
    run.add_argument("--api-key-env")
    run.add_argument("--lms-path", dest="fit_lms_path", type=Path)
    run.add_argument("--models-root", dest="fit_models_root", type=Path)
    run.add_argument("--recovery-confirm")
    run.add_argument("--recovery-note")
    run.add_argument("--out", type=Path, required=True)
    compare = actions.add_parser("compare", help="compare sealed diagnostics offline")
    compare.set_defaults(command="engine-fit compare")
    compare.add_argument("--runs", dest="report_runs", nargs="+", type=Path, required=True)
    compare.add_argument("--out", type=Path, required=True)
    verify = actions.add_parser("verify", help="verify diagnostic or comparison evidence offline")
    verify.set_defaults(command="engine-fit verify")
    verify.add_argument("--path", dest="run", type=Path, required=True)
    verify.add_argument("--rerender", action="store_true")
