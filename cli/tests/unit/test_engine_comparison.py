from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.contracts.engine_comparison import PENDING
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


def real_local_shape(family, **model):
    data = local(family, **model)
    data["identity"]["runtime_library_hashes"] = None
    data["config"]["model"]["sha256"] = None
    data["config"]["conditions"]["cache_policy"] = "unknown"
    return data
