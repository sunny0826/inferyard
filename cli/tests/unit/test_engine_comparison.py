from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.engine_comparison import PENDING
from tests.unit.test_comparison import trial


def local(family, **model):
    data = trial()
    data["plan"]["experiment"]["comparison"] = {"mode": "engine", "factor": None}
    data["config"]["definition"] = "local-engine-config.v1"
    data["config"]["engine"] = {
        "adapter": "local_openai_v1",
        "family": family,
        "backend": "metal",
        "installation_sha256": family,
        "startup_args": [family],
    }
    data["config"]["model"].update(
        {"kind": "gguf", "content_sha256": "g", "assets_sha256": "a", **model}
    )
    data["identity"]["runtime_library_hashes"] = {"lib": family}
    return data


def statuses(result):
    return {row["field"]: row["status"] for row in result["conditions"]}


def test_same_gguf_pair_allows_only_engine_fields_and_stays_pending():
    result = compare_trials(local("llama-cpp"), local("lmstudio"))
    rows = statuses(result)
    assert rows["config.engine.family"] == "allowed_difference"
    assert rows["config.engine.installation_sha256"] == "allowed_difference"
    assert rows["config.model.content_sha256"] == "same"
    assert "config.engine.release" not in rows
    assert result["blockers"] == list(PENDING)
    assert result["eligibility"]["performance"] is False


@pytest.mark.parametrize(
    "left,right,blocker",
    [
        (local("llama-cpp"), local("llama-cpp"), "engine_family_did_not_change"),
        (local("llama-cpp"), local("vllm"), "unsupported_engine_pair"),
        (
            local("llama-cpp"),
            local("lmstudio", kind="directory"),
            "engine_mode_requires_single_gguf",
        ),
        (local("llama-cpp"), local("lmstudio", content_sha256="other"), None),
    ],
)
def test_engine_mode_rejections(left, right, blocker):
    result = compare_trials(left, right)
    if blocker is None:
        assert "config.model.content_sha256:different" in result["blockers"]
    else:
        assert blocker in result["blockers"]


def test_engine_mode_requires_local_profile_on_both_sides():
    other = deepcopy(trial())
    other["plan"]["experiment"]["comparison"] = {"mode": "engine", "factor": None}
    result = compare_trials(local("llama-cpp"), other)
    assert "engine_mode_requires_local_profile" in result["blockers"]


def test_other_modes_keep_local_profile_blocked():
    result = compare_trials(local("llama-cpp"), local("lmstudio"), mode="side-by-side")
    assert "local_engine_controlled_comparison_not_supported" in result["blockers"]


def test_cross_format_pair_allows_model_asset_differences():
    mlx = local(
        "mlx-lm", kind="directory", content_sha256="m", assets_sha256="ma", packing="directory"
    )
    result = compare_trials(local("llama-cpp"), mlx)
    rows = statuses(result)
    assert rows["config.model.content_sha256"] == "allowed_difference"
    assert rows["config.model.kind"] == "allowed_difference"
    assert result["blockers"] == list(PENDING)


def test_same_asset_pair_still_rejects_model_differences():
    result = compare_trials(local("llama-cpp"), local("lmstudio", packing="other"))
    assert "config.model.packing:different" in result["blockers"]


def real_local_shape(family, **model):
    # The local schema never fills these; real runs carry None or "unknown".
    data = local(family, **model)
    data["identity"]["runtime_library_hashes"] = None
    data["config"]["model"]["sha256"] = None
    data["config"]["conditions"]["cache_policy"] = "unknown"
    return data


def test_local_profile_legacy_fields_use_their_replacements():
    result = compare_trials(real_local_shape("llama-cpp"), real_local_shape("lmstudio"))
    rows = statuses(result)
    for field in (
        "identity.runtime_library_hashes",
        "config.model.sha256",
        "config.conditions.cache_policy",
    ):
        assert field not in rows
    assert result["blockers"] == list(PENDING)


def test_local_profile_replacement_digest_stays_mandatory():
    left = real_local_shape("llama-cpp", content_sha256=None)
    result = compare_trials(left, real_local_shape("lmstudio"))
    assert "config.model.content_sha256:unknown" in result["blockers"]
