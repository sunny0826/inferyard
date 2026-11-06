"""公开诊断入口在真实执行尚未开放时拒绝启动。"""

from scripts.ninfer_source_host.probe_fixture import main


def test_main_returns_two_without_fixture_or_environment(monkeypatch):
    calls = []
    monkeypatch.setattr(
        "scripts.ninfer_source_host.probe_fixture.run_fixture",
        lambda *args, **kwargs: calls.append(1),
    )
    monkeypatch.setenv("LAB46_ROOT", "C:" + chr(92) + "escape")
    assert main([]) == 2
    assert main(["probe", "file_io", "{}"]) == 2
    assert calls == []
