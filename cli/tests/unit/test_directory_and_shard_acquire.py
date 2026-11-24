"""Security boundary tests for remote directory-model acquisition."""

from __future__ import annotations

import httpx
import pytest

from inferyard.provisioning.model_source_download import acquire
from inferyard.provisioning.preparation_io import PreparationError

PINNED_SHA = "73e3e38d981303bc594367cd910ea6eb48349da8"


def test_acquire_directory_rejects_traversal_and_incomplete_tree(tmp_path):
    def traversal_handler(_request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json=[{"type": "file", "path": "../escape.safetensors", "size": 10}],
        )

    with httpx.Client(transport=httpx.MockTransport(traversal_handler)) as client:
        with pytest.raises(PreparationError) as exc:
            acquire(
                f"https://huggingface.co/mlx-community/bad/tree/{PINNED_SHA}",
                tmp_path / "out1",
                client=client,
            )
        assert exc.value.details["failure_cause"] == "invalid_directory_entry"

    def missing_weights_handler(_request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=[{"type": "file", "path": "config.json", "size": 2}])

    with httpx.Client(transport=httpx.MockTransport(missing_weights_handler)) as client:
        with pytest.raises(PreparationError) as exc2:
            acquire(
                f"https://huggingface.co/mlx-community/incomplete/tree/{PINNED_SHA}",
                tmp_path / "out2",
                client=client,
            )
        assert exc2.value.details["failure_cause"] == "incomplete_model_directory"
