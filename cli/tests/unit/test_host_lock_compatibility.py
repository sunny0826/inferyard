"""Native Windows Common AppData lookup ignores per-user environment overrides."""

from types import SimpleNamespace

from inferyard.platforms.windows_host_paths import common_data_root


def test_system_folder_comes_from_native_common_appdata_not_user_environment(monkeypatch):
    seen = []

    def query(window, folder, token, flags, result):
        seen.append((window, folder, token, flags))
        result.value = "C:/ProgramData"
        return 0

    class Query:
        def __call__(self, *args):
            return query(*args)

    monkeypatch.setenv("ProgramData", "Z:/per-user")
    monkeypatch.setattr(
        "inferyard.platforms.windows_host_paths.ctypes.WinDLL",
        lambda *args, **kwargs: SimpleNamespace(SHGetFolderPathW=Query()),
        raising=False,
    )
    assert str(common_data_root()).replace("\\", "/") == "C:/ProgramData"
    assert seen == [(None, 0x0023, None, 0)]
