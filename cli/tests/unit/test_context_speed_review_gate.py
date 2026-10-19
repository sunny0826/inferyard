"""Public preparation rejection preserves the single approved review reason."""

import json

import pytest

from inferyard.cli import main
from inferyard.config import community_workspace as workspace
from inferyard.config.preparation_io import PreparationError
from inferyard.contracts.validation import ContractError


def test_public_review_reason(tmp_path, capsys, monkeypatch):
    from scripts.build_context_speed_bundle import BUNDLE_NAME, outputs

    monkeypatch.setattr(workspace, "resource_bytes", lambda _: outputs()[BUNDLE_NAME])
    out = tmp_path / "refused"
    assert main(["init", "--bundle", "zh-context-speed", "--out", str(out)]) == 4
    response = json.loads(capsys.readouterr().out)
    assert response["limitations"] == ["human_corpus_review_required"]
    assert response["status"] == "error"
    assert response["completeness"] == "incomplete"
    assert response["details"] is None
    assert not out.exists()


def test_default_bundles_still_prepare(tmp_path, capsys):
    out = tmp_path / "ordinary"
    assert main(["init", "--out", str(out)]) == 0
    response = json.loads(capsys.readouterr().out)
    assert [v["bundle"] for v in response["details"]["bundles"]] == list(workspace.DEFAULT_BUNDLES)


@pytest.mark.parametrize("raw", [b"{", b"{}", b"null", b'{"schema_version":true}'])
def test_malformed_resource_is_not_review(monkeypatch, raw):
    monkeypatch.setattr(workspace, "resource_bytes", lambda _: raw)
    with pytest.raises(PreparationError) as caught:
        workspace._load_bundle("zh-context-speed")
    assert caught.value.reason == "package_resource_invalid"


@pytest.mark.parametrize(
    "path,reason",
    [
        ("bundle.other", "human_corpus_review_required"),
        ("bundle.review_records", "damaged_review"),
        ("bundle.review_records", "human_corpus_review_required private text"),
    ],
)
def test_other_contract_errors_remain_resource_errors(monkeypatch, path, reason):
    def reject(_):
        raise ContractError(path, reason)

    monkeypatch.setattr(workspace, "require_review", reject)
    with pytest.raises(PreparationError) as caught:
        workspace._load_bundle("zh-context-speed")
    assert caught.value.reason == "package_resource_invalid"


def test_resource_hash_gate_remains_before_review(tmp_path, monkeypatch):
    from inferyard.config import community_resources as resources

    root = tmp_path / "data" / "community"
    (root / "bundles").mkdir(parents=True)
    (root / "resources.json").write_text(json.dumps({"bundles/zh-context-speed.json": "0" * 64}))
    (root / "bundles" / "zh-context-speed.json").write_bytes(b"{}")
    monkeypatch.setattr(resources, "files", lambda _: tmp_path)
    with pytest.raises(PreparationError) as caught:
        workspace._load_bundle("zh-context-speed")
    assert caught.value.reason == "package_resource_invalid"
    assert caught.value.code == 4
