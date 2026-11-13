"""Shared application requests and results, independent of CLI presentation."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from inferyard.config.loader import LoadedConfig


@dataclass(frozen=True, slots=True)
class CommandRequest:
    command: str
    config: LoadedConfig | None = None
    from_run: Path | None = None
    run: Path | None = None
    left: Path | None = None
    right: Path | None = None
    out: Path | None = None
    diagnostic: bool = False
    endpoint_url: str | None = None
    server_pid: int | None = None
    output_root: Path | None = None
    api_key_env: str | None = None
    recovery_confirm: str | None = None
    recovery_note: str | None = None
    experiment: Path | None = None
    dry_run: bool = False
    catalogue_kind: str | None = None
    frozen_plan: Path | None = None
    workload_id: str | None = None
    handoff_note: str | None = None
    service_binding_path: Path | None = None
    trial_id: str | None = None
    tolerance_ratio: float | None = None
    max_wall_seconds: float | None = None
    comparison_mode: str | None = None
    filter_spec: Path | None = None
    report_runs: list[Path] | None = None
    left_runs: list[Path] | None = None
    right_runs: list[Path] | None = None
    left_configs: list[Path] | None = None
    right_configs: list[Path] | None = None
    bundles: list[str] | None = None
    comparison_experiment_id: str | None = None
    analysis_path: Path | None = None
    scorer_id: str | None = None
    revision_reason: str | None = None
    length_spec: Path | None = None
    target_run: Path | None = None
    common_observer: bool = False
    boundary_observer: bool = False
    first_event_tolerance_ratio: float | None = None
    engine_rate_tolerance_ratio: float | None = None
    block_gap_tolerance_ms: float | None = None
    left_overhead: Path | None = None
    right_overhead: Path | None = None
    extension_spec: Path | None = None
    models_roots: list[Path] | None = None
    model_path: Path | None = None
    fit_engines: list[str] | None = None
    fit_engine: str | None = None
    fit_prompts: Path | None = None
    fit_served_model: str | None = None
    fit_max_tokens: int = 128
    fit_timeout: float = 60.0
    fit_repetitions: int = 1
    fit_min_memory_mib: int = 512
    fit_lms_path: Path | None = None
    fit_models_root: Path | None = None
    fit_temperature_stop_override_reason: str | None = None
    fit_memory_stop_override_reason: str | None = None
    source_roots: list[str] | None = None
    community_bundle: str | None = None
    benchmark_profile: str | None = None
    context_size: int | None = None
    preset: str | None = None
    batch: Path | None = None
    runtime_profile: str | None = None
    archive: Path | None = None
    runtime_archive: Path | None = None
    installation: Path | None = None
    served_model: str | None = None
    max_tokens: int | None = None
    timeout_seconds: float | None = None
    engine_path: Path | None = None
    runtime_receipt: Path | None = None
    preflight: Path | None = None
    bundle_path: Path | None = None
    candidate: Path | None = None
    model_repo: str | None = None
    model_revision: str | None = None
    port: int | None = None
    model_source_url: str | None = None
    model_token_env: str | None = None
    model_timeout_seconds: float = 1800
    model_source_record: Path | None = None
    engine_source_url: str | None = None
    engine_timeout_seconds: float = 1800
    protect_pids: list[int] | None = None
    workspace: Path | None = None
    temperature_stop_override_reason: str | None = None
    prompt_cache: str | None = None


@dataclass(frozen=True, slots=True)
class CommandResult:
    command: str
    status: str
    completeness: str = "incomplete"
    run_id: str | None = None
    evidence_dir: str | None = None
    limitations: tuple[str, ...] = ()
    details: dict | None = None


Handler = Callable[[CommandRequest], tuple[int, CommandResult]]
