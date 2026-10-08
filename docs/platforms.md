# 平台状态

本页区分实现、历史原生覆盖与安装验证。`v0.0.1` 发布状态见
[Releases](https://github.com/sunny0826/inferyard/releases)，安装检查来源由 [backlog](backlog.md)跟踪。
历史覆盖来自 2026-10-02 至 10-07 的已保存运行，
不自动转移到新源码或候选 wheel，解释规则见[证据血缘](data-contract.md#证据血缘与比较结论)。

## 实现范围

| 能力 | Linux x64 | macOS arm64 | Windows x64 |
| --- | --- | --- | --- |
| init、设备检测、绑定 | 已实现 | 已实现 | 已实现 |
| 包内候选准备 | config assets + 手工模板 | config create，CPU/Metal | runtime prepare + config create，CPU/CUDA |
| 单次及批量 | 已实现，按协议预检 | 已实现，按协议预检 | Prism/KVMem/NInfer；后两者限固定质量串行文本 |
| 基础采集 | CPU、RSS、可用内存及可得传感器 | CPU、RSS、可用内存、主机换页、只读 AppleSMC 温度 | Prism：windows-resource.v1；KVMem/NInfer：windows-memory.v1；RSS/可用内存，其他指标缺测 |
| engine-fit | vLLM/SGLang/llama.cpp，逐引擎能力约束 | llama.cpp/MLX-LM/LM Studio，逐资产约束 | 单 GGUF llama.cpp 诊断 |
| 离线报告与核验 | 已实现 | 已实现 | 已实现 |

Ollama 缺少可核验的全服务空闲来源，engine-fit 执行仍阻断。
Windows 的 live 开销、实时扩展和 prepare-length 等入口仍有平台限制；详见 [Windows](../WINDOWS.md)。

## 历史原生覆盖

| 平台/组合 | 已保存覆盖 | 主要限制与来源定位 |
| --- | --- | --- |
| macOS M4、Prism/Metal | Bonsai 完整 120 题、Qwen3-4B 两轮、报告/核验；SVG 展示 | Bonsai 来源提交 `d6f6d80`，Qwen `746149e`；完整开销历史对照仍未取得严格性能资格 |
| Windows x64、Prism/CUDA | Qwen3-4B 两轮各 120 completed，采集/封存/report/verify | 实现 `1556261`、原生轮次 `5f040aa`；温度/频率/功耗/能耗未采集 |
| Windows x64、KVMem/NInfer | 各两轮 120 题、取消子集和新进程恢复 | formal09 来源 `8132410`；模板预算、有效参数及引擎内部排空仍有缺测 |
| Linux x64、Prism/CPU | 小模型诊断；Bonsai 与 Qwen3-4B 完整题包的温控豁免诊断 | 来源 `67f1a97`、`3644e1f`；正常计划发生温停，豁免为 record-only，正式覆盖仍待补 |
| inferyard-observer | macOS arm64 原生监测及合成服务关联 | 六目标交叉编译不代表 Linux/Windows/macOS amd64 原生运行通过 |

上表提交号属于开发源仓库，仅供原操作者追溯；InferYard 的全新 Git 历史不包含这些提交或原始证据。历史覆盖不构成重命名后安装包的原生验证。
各端设备、后端、采集器与测量源码不同，不能凭此表做跨端速度排名。

## 当前候选验证与未验项

- [PR #1 的 CI](https://github.com/sunny0826/inferyard/actions/runs/37716506876) 已完成 Linux x64、
  Windows x64、macOS arm64 的源码外安装检查，包括 uv tool/uvx、中文工作区、离线报告、当前/旧格式边界
  及合成升级/回退。最终发行批次的来源和摘要以 Release 说明及候选清单为准。
- 同一 CI 在一次性原生 runner 上验证固定路径的首次初始化、合成请求成功、进程中断后 dirty 持久化及拒绝。
  没有使用真实模型，也未覆盖旧状态迁移的全部原生场景。
- Windows 的旧状态迁移、ACL/reparse/多账户/卷变化、无 D 盘环境及原生 runtime prepare/create 仍待专项验证。
- 第二架构、所有模型、长时负载、并发和严格性能资格均不由现有安装检查推定。

使用入口：[安装](installation.md) · [macOS](../MACOS.md) · [Windows](../WINDOWS.md) · [配置](../configs/README.md)。
