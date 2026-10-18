"""Cohorts use stable observed hardware, not incidental resource samples."""

from inferyard.analysis.context_speed_cohort import cohort
from tests.unit.test_context_speed_profile import inputs as inputs_fixture

inputs = inputs_fixture


def test_cuda_telemetry_changes_do_not_split_identity(inputs):
    _, config, _ = inputs
    config["engine"]["backend"] = "cuda"
    environment = dict(
        platform="Windows",
        architecture="x64",
        kernel="fixture",
        cpu_model="fixture",
        logical_cpus=8,
        memory_total_bytes=1234,
        gpu=dict(
            backend="cuda",
            devices=[
                dict(
                    index=0,
                    name="fixture",
                    driver_version="1",
                    memory_total_bytes=4096,
                    memory_free_bytes=2048,
                    temperature_c=40,
                    power_w=1,
                )
            ],
        ),
    )
    token = dict(actual_input_tokens=100, template_prompt_sha256="a" * 64)
    original = cohort(config, environment, token)
    assert original
    environment["gpu"]["devices"][0].update(memory_free_bytes=1024, temperature_c=50, power_w=10)
    assert cohort(config, environment, token) == original
    environment["gpu"]["devices"][0]["driver_version"] = "2"
    assert cohort(config, environment, token) != original
    environment["gpu"]["devices"][0]["driver_version"] = "unknown"
    assert cohort(config, environment, token) is None


def test_missing_hardware_never_gets_common_unknown_cohort(inputs):
    _, config, _ = inputs
    assert cohort(config, {}, dict(actual_input_tokens=100)) is None
