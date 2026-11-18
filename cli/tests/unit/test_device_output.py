"""Explicit device presentation covers errors, cancellation and application boundaries."""

import json
from copy import deepcopy

import pytest

from inferyard.application.types import CommandResult
from inferyard.cli import main
from inferyard.platforms import device_preflight
from tests.unit.test_device_preflight import GIB, gguf, hardware, models


@pytest.fixture
def host(monkeypatch):
    value = hardware()
    value.update(
        platform="Linux",
        architecture="x86_64",
        cpu_model="Test CPU",
        memory_total_bytes=16 * GIB,
        disk_path="/output-volume",
    )
    monkeypatch.setattr(device_preflight, "hardware_snapshot", lambda path: deepcopy(value))
    return value


@pytest.mark.parametrize(
    "available,disk,reason",
    [
        (None, 40 * GIB, "host_memory_unavailable"),
        (GIB, 40 * GIB, "insufficient_host_memory"),
        (12 * GIB, None, "output_disk_unavailable"),
        (12 * GIB, GIB, "insufficient_output_disk"),
    ],
)
def test_recommendation_distinguishes_missing_and_insufficient_capacity(available, disk, reason):
    state = hardware()
    state.update(memory_available_bytes=available, disk_free_bytes=disk)
    report = device_preflight.recommend(state, models())
    assert report["status"] == "blocked" and report["reason"] == reason
    assert report["selected"] is None and report["limitations"]
