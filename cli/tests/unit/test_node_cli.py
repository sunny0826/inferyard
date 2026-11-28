import json

from inferyard.cli import main
from inferyard.node.join import PairingError


def test_join_rejection_has_exit_two_and_stable_limitation(capsys, monkeypatch):
    def rejected(**kwargs):
        assert kwargs["code"] == "839204"
        raise PairingError("pair_rejected")

    monkeypatch.setattr("inferyard.node.join.join", rejected)
    code = main(
        [
            "join", "--hub", "127.0.0.1:48860", "--code", "839-204",
            "--ca-sha256", "ab" * 32, "--state-dir", "unused", "--no-autostart", "--json",
        ]
    )
    result = json.loads(capsys.readouterr().out)
    assert code == 2
    assert result["status"] == "blocked"
    assert result["limitations"] == ["pair_rejected"]


def test_join_rejects_malformed_code_without_loading_backend(capsys):
    code = main(
        ["join", "--hub", "127.0.0.1:48860", "--code", "12", "--ca-sha256", "ab" * 32]
    )
    result = json.loads(capsys.readouterr().out)
    assert code == 2
    assert result["limitations"] == ["invalid_input"]
