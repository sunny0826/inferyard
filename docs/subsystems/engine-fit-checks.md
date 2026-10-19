# engine-fit 验证

本文属于 [engine-fit 引擎诊断](engine-fit.md)。

## 验证

在仓库根 `cd cli` 后执行；测试使用 fixtures 与模拟服务，不发送真实模型请求。

```sh
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_common_plan.py tests/unit/test_engine_fit_plan.py tests/unit/test_engine_fit_common_identity.py tests/unit/test_engine_fit_identity.py tests/unit/test_engine_fit_transport.py tests/unit/test_engine_fit_common_transport.py tests/unit/test_engine_fit_common_evidence.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_temperature_override.py tests/unit/test_engine_fit_memory_override.py tests/unit/test_engine_fit_report.py tests/unit/test_engine_fit_partial.py tests/unit/test_engine_fit_native_evidence.py tests/unit/test_engine_fit_async_guard.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_lmstudio.py tests/unit/test_engine_fit_lmstudio_files.py tests/unit/test_engine_fit_macos_listener.py tests/unit/test_engine_fit_macos_identity.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_mlx_server.py tests/unit/test_engine_fit_mlx_stream.py tests/unit/test_engine_fit_mlx_context.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_windows_identity.py tests/unit/test_engine_fit_windows_file_hash.py tests/unit/test_engine_fit_windows_temperature.py tests/unit/test_engine_fit_windows_evidence.py
mise exec -- uv run --frozen pytest -q tests/integration/test_engine_fit.py tests/integration/test_engine_fit_common.py tests/integration/test_engine_fit_macos.py tests/integration/test_engine_fit_temperature_override_flow.py tests/integration/test_engine_fit_memory_override_flow.py tests/integration/test_engine_fit_windows.py
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen inferyard engine-fit engines
```

`engine-fit` 相关测试文件位于 `tests/unit/` 与 `tests/integration/`，按平台与主题分组；
pytest 与 ruff 由 `pyproject.toml` 的 dev 依赖组提供（pytest 8.x、ruff 0.12+，Ruff 目标
Python 3.15、行宽 100）；`inferyard engine-fit engines` 为离线能力表，可作为安装烟测。
集成测试中涉及本地 TCP 模拟的用例需要 socket 权限；沙箱阻塞应与实现失败分开报告。
