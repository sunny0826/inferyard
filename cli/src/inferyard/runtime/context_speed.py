"""Special profile admission; all execution remains in the ordinary runner."""

from inferyard.config.context_speed_profile import BUNDLE_ID, CASES, classify_verified_inputs
from inferyard.platforms.identity import PreflightError


def is_speed(plan, loaded):
    return any(
        classify_verified_inputs(plan, item.config.to_dict(), item.bundle.to_dict()) is not None
        for item in loaded.values()
    )


def reject_single(bundle):
    if bundle["bundle_id"] == BUNDLE_ID or any(
        c["case_id"] in CASES.values() for c in bundle["cases"]
    ):
        raise PreflightError("context_speed_requires_new_plan_and_batch")


def reject_rerun(data):
    if classify_verified_inputs(data["plan"], data["config"], data["bundle"]):
        raise PreflightError("context_speed_requires_new_plan_and_batch")
