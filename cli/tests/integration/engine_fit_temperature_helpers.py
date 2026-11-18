"""Temperature override flow with synthetic hot sensors; no models or live engines."""

import shutil
from copy import deepcopy

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import read_json
from tests.integration import test_engine_fit_common as common_fixtures
from tests.unit.test_engine_fit_native_evidence import native_data

REASON = 'explicit short diagnostic <script>reason</script> & "approval"'
common_fit_service = common_fixtures.common_fit_service


@pytest.fixture
def override_service(common_fit_service, monkeypatch):
    from inferyard.platforms import sensors_macos

    sample = native_data.__wrapped__()[1]["resources"][0]["temperature_samples"][0]

    class SyntheticSensors:
        sources = [{"metric_name": "temperature"}]

        def collect(self, _phase, _request):
            values = []
            for temperature in (95.5, 42.5, None):
                value = deepcopy(sample)
                value.update(raw_value=temperature, value=temperature)
                value["missing_reason"] = (
                    "synthetic_source_unavailable" if temperature is None else None
                )
                values.append(value)
            return values

    monkeypatch.setattr(sensors_macos, "MacSensors", SyntheticSensors)
    return common_fit_service
