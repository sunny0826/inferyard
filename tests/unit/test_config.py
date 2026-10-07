"""F01 input validation; no model, process or network needed."""

from pathlib import Path

import pytest

from inferyard.config.loader import load_config, validate_endpoint
from inferyard.contracts.validation import ContractError


@pytest.mark.parametrize(
    "fixture,field",
    [
        ("unknown-field.toml", "unexpected"),
        ("concurrency.toml", "execution.concurrency"),
        ("negative-timeout.toml", "execution.timeout_seconds"),
        ("plaintext-key.toml", "endpoint.api_key"),
        ("duplicate-case.toml", "bundle.cases"),
    ],
)
def test_required_invalid_fixtures(config_path, fixture, field):
    with pytest.raises(ContractError) as error:
        load_config(config_path.with_name(fixture))
    assert field in error.value.path
    assert "fixture-secret-7f39" not in str(error.value)


def test_paths_are_relative_to_config_not_working_directory(config_path, tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    loaded = load_config(config_path.resolve())
    data = loaded.config.to_dict()
    assert Path(data["bundle"]["path"]).is_file()
    assert data["model"]["local_path"] == str(config_path.parent / "fixture.gguf")
    assert not Path(data["model"]["local_path"]).exists()  # T02, not T01, checks model identity.
    assert data["execution"]["concurrency"] == 1
    assert data["execution"]["warmup_count"] == 3
    assert data["telemetry"]["interval_ms"] == 500
    assert "execution.timeout_seconds" in loaded.defaulted_fields
    assert len(loaded.input_sha256) == len(loaded.bundle_sha256) == 64


@pytest.mark.parametrize(
    "endpoint",
    [
        "http://127.0.0.1:8080",
        "https://localhost:443/",
        "http://[::1]:8080",
    ],
)
def test_loopback_origins(endpoint):
    validate_endpoint(endpoint)


@pytest.mark.parametrize(
    "endpoint",
    [
        "http://example.com",
        "http://192.168.1.1",
        "file:///tmp/a",
        "http://127.0.0.1:0",
        "http://user:fixture-secret-7f39@localhost",
        "http://localhost?api_key=fixture-secret-7f39",
        "http://localhost/#anything",
        "http://localhost/v1",
        "http://localhost:invalid",
        "http://localhοst",
        "http://localhost\n",
        "http://[::ffff:192.168.1.1]",
    ],
)
def test_endpoint_rejections_do_not_echo_values(endpoint):
    with pytest.raises(ContractError) as error:
        validate_endpoint(endpoint)
    assert "fixture-secret-7f39" not in str(error.value)


def test_malformed_toml_redacts_parser_message(tmp_path):
    path = tmp_path / "bad.toml"
    path.write_text("endpoint = fixture-secret-7f39")
    with pytest.raises(ContractError) as error:
        load_config(path)
    assert "fixture-secret-7f39" not in str(error.value)


def test_bundle_version_mismatch(config_path, tmp_path):
    text = config_path.read_text()
    bundle_path = config_path.parent.parent / "contracts" / "bundle.valid.json"
    text = text.replace("../contracts/bundle.valid.json", bundle_path.as_posix())
    text = text.replace('version = "fixture-v1"', 'version = "different"')
    path = tmp_path / "bad-version.toml"
    path.write_text(text)
    with pytest.raises(ContractError, match="config.bundle.version"):
        load_config(path)


def test_schema_validation_precedes_bundle_file_access(config_path, tmp_path):
    text = config_path.read_text().replace("../contracts/bundle.valid.json", "/missing/bundle")
    text = text.replace("[endpoint]", '[endpoint]\napi_key = "fixture-secret-7f39"')
    path = tmp_path / "secret.toml"
    path.write_text(text)
    with pytest.raises(ContractError, match="endpoint.api_key"):
        load_config(path)


def test_startup_arguments_cannot_smuggle_plaintext_credentials(config_path, tmp_path):
    text = config_path.read_text().replace(
        "startup_args = []", 'startup_args = ["--api-key=fixture-secret-7f39"]'
    )
    path = tmp_path / "secret-argument.toml"
    path.write_text(text)
    with pytest.raises(ContractError, match="startup_args") as error:
        load_config(path)
    assert "fixture-secret-7f39" not in str(error.value)
