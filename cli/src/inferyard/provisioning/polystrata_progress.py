"""Progress stream line parser for PolyStrata setup.py output."""

from __future__ import annotations

import re

from inferyard.provisioning.transfer_progress import ProgressEmitter

_STEP_RE = re.compile(r"^===\s*Step\s+(\d+)\s*:\s*(.+?)\s*===$", re.IGNORECASE)
_DOWNLOAD_LINE_RE = re.compile(
    r"^\s*(.+?):\s*([0-9]+(?:\.[0-9]+)?)\s*/\s*([0-9]+(?:\.[0-9]+)?)\s*"
    r"(GB|GiB|MB|MiB|KB|KiB)\s*\(([0-9]+(?:\.[0-9]+)?)%\)",
    re.IGNORECASE,
)
_DOWNLOAD_UNKNOWN_TOTAL_RE = re.compile(
    r"^\s*(.+?):\s*([0-9]+(?:\.[0-9]+)?)\s*(GB|GiB|MB|MiB|KB|KiB)\s*$",
    re.IGNORECASE,
)
_NON_DOWNLOAD_SIZE_LABELS = (
    "memory",
    "ram",
    "vram",
    "disk",
    "free",
    "available",
    "total",
    "size",
)
_CMAKE_PCT_RE = re.compile(r"^\[\s*(\d{1,3})%\]\s*(.*)$")
_NINJA_STEP_RE = re.compile(r"^\[(\d+)/(\d+)\]\s*(.*)$")
_UNIT_MULTIPLIERS = {
    "kb": 1_000,
    "kib": 1024,
    "mb": 1_000_000,
    "mib": 1024**2,
    "gb": 1_000_000_000,
    "gib": 1024**3,
}


def parse_setup_line(
    line: str,
    current_stage: str,
    current_step: int | None,
    emitter: ProgressEmitter,
) -> tuple[str, int | None]:
    stripped = line.strip()
    if not stripped:
        return current_stage, current_step
    base_step = 4 if (current_step or 0) >= 4 else 3
    step_match = _STEP_RE.match(stripped)
    if step_match is not None:
        step_num = int(step_match.group(1))
        title = step_match.group(2).strip()
        detail = f"Step {step_num}: {title}"
        is_model_download = step_num == 5 or (
            "download" in title.lower() and (step_num >= 5 or "model" in title.lower())
        )
        if is_model_download:
            next_stage, outer_step = "downloading_model", 4
        elif (
            step_num >= 6
            or current_stage in ("downloading_model", "verifying")
            or base_step >= 4
        ):
            next_stage, outer_step = "verifying", 4
        else:
            next_stage, outer_step = "setup_engine", 3
        emitter.update(
            next_stage,
            0,
            None,
            step=outer_step,
            step_total=4,
            detail=detail,
            force=True,
        )
        return next_stage, outer_step

    dl_match = _DOWNLOAD_LINE_RE.match(stripped)
    if dl_match is not None:
        label = dl_match.group(1).strip()
        mult = _UNIT_MULTIPLIERS[dl_match.group(4).lower()]
        have_bytes = int(float(dl_match.group(2)) * mult)
        raw_total = int(float(dl_match.group(3)) * mult)
        total_bytes = raw_total if raw_total > 0 else None
        dl_stage = (
            "downloading_model"
            if (current_stage == "downloading_model" or base_step >= 4)
            else "setup_engine"
        )
        outer_step = 4 if dl_stage == "downloading_model" else 3
        emitter.update(
            dl_stage,
            have_bytes,
            total_bytes,
            step=outer_step,
            step_total=4,
            percent=float(dl_match.group(5)),
            detail=label,
        )
        return dl_stage, outer_step

    if base_step >= 4:
        unknown_dl_match = _DOWNLOAD_UNKNOWN_TOTAL_RE.match(stripped)
        if unknown_dl_match is not None:
            label = unknown_dl_match.group(1).strip()
            if label.lower() not in _NON_DOWNLOAD_SIZE_LABELS:
                mult = _UNIT_MULTIPLIERS[unknown_dl_match.group(3).lower()]
                have_bytes = int(float(unknown_dl_match.group(2)) * mult)
                emitter.update(
                    "downloading_model",
                    have_bytes,
                    None,
                    step=4,
                    step_total=4,
                    detail=label,
                )
                return "downloading_model", 4

    cmake_match = _CMAKE_PCT_RE.match(stripped)
    if cmake_match is not None:
        stage = current_stage if base_step >= 4 else "setup_engine"
        emitter.update(
            stage,
            0,
            None,
            step=base_step,
            step_total=4,
            percent=float(cmake_match.group(1)),
            detail=cmake_match.group(2).strip() or stripped,
        )
        return stage, base_step

    ninja_match = _NINJA_STEP_RE.match(stripped)
    if ninja_match is not None:
        done, total = int(ninja_match.group(1)), int(ninja_match.group(2))
        if total > 0:
            stage = current_stage if base_step >= 4 else "setup_engine"
            emitter.update(
                stage,
                0,
                None,
                step=base_step,
                step_total=4,
                percent=(100.0 * done) / total,
                detail=ninja_match.group(3).strip() or stripped,
            )
            return stage, base_step

    if current_stage == "downloading_model":
        lower = stripped.lower()
        if (
            lower.startswith("checking ")
            or lower.startswith("verifying ")
            or "sha-256" in lower
            or "sha256" in lower
        ):
            emitter.update(
                "verifying",
                0,
                None,
                step=4,
                step_total=4,
                detail=stripped,
                force=True,
            )
            return "verifying", 4

    if current_stage in ("setup_engine", "verifying") and any(ch.isalnum() for ch in stripped):
        emitter.update(
            current_stage,
            0,
            None,
            step=base_step,
            step_total=4,
            detail=stripped,
            force=False,
        )
        return current_stage, base_step

    return current_stage, current_step
