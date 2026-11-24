<p align="left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-mark-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo-mark-light.svg">
    <img src="assets/logo-mark-light.svg" alt="InferYard logo" width="64" height="64">
  </picture>
</p>

# InferYard

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)

[English](README.md) | 中文

**在自己的硬件上测本地大模型：结果可复现、资源曲线实时可见、每份原始证据都留得住。**

公开榜单只能反映模型在别人集群上的表现，说明不了某款量化模型在你手头的电脑和推理引擎上到底答得准不准、跑得快不快、吃多少内存。InferYard 将确定性测评放进原生桌面应用（**InferYard Studio**），由配套 **CLI** 执行子项目驱动：用人工审核并冻结的中文题集和长上下文速度测试跑本地服务，实时记录 CPU、内存和可用温度曲线，把每道题的答案、耗时和硬件口径完整留在本地磁盘，生成清晰直观的原生报告——没有云端上传，不用模型当裁判，也不用单一总分掩盖真实取舍。

## 项目核心能力

- **结果可复现，不靠模型当裁判。** 用冻结且经过人工审核的题集测试本地模型，按各题适用的固定规则评分，避免裁判模型波动带来的不确定性。
- **不做虚假“总分”，直面真实取舍。** 质量、请求延迟、吞吐以及 CPU、内存和可用温度采集分维度独立呈现；无法测量的指标保持为 `null` 并标明原因，绝不补 `0`。
- **完整保留原始证据。** 逐题答案、执行元数据与资源采样均落盘封存，服务关闭后依然可以离线复核，并投影出供 Studio 展示的 `studio.json` 报告。
- **原生桌面工作台。** [InferYard Studio](studio/README.md) 基于 Go 与 MyGo 构建原生桌面界面，涵盖以下能力：
  - **多语言界面（i18n）：** 内置英文与简体中文（在「设置 → 语言」中切换「跟随系统」/「English」/「简体中文」，无需重启即时生效；支持 `MYGO_LOCALE` 环境变量覆盖及 macOS 应用包本地化）。
  - **桌面交互与自更新：** 浅色 / 深色 / 跟随系统主题实时可视化预览、关闭窗口最小化到系统托盘（后台继续运行测评与下载）、独立作用域的 HTTP(S) 下载代理，以及自动检查 GitHub Releases 更新与一键 SHA-256 校验自更新。
  - **硬件感知下载与资产管理：** 硬件感知首装推荐（在 Apple Silicon、NVIDIA CUDA、AMD ROCm 硬件上优先推荐 **PolyStrata**，支持一键 4 阶段引导安装与恢复流程，其余设备平滑回退至 `llama.cpp`）、可扩展多引擎目录（`PolyStrata`、`llama.cpp`、`mlx-lm`、`vLLM`、`SGLang`、`LM Studio`）、Hugging Face 与 ModelScope 镜像、自定义 URL 解析（`model resolve`）、多分片 GGUF 与目录模型流式进度下载与断点续传，以及多根目录模型管理与本地 PolyStrata 运行时及模型的安全卸载和磁盘清理。
  - **任务编排与实时监控：** 自动检测 CLI 版本并支持一键「升级 CLI」（要求 `uv >= 0.13.6`）、常规题包与「长上下文专项」（`zh-context-speed`：64K / 128K / 256K）、引擎状态卡片与 Web UI 直达链接、实时「资源曲线」、应用内通知收件箱与系统通知，以及按模型分组的报告浏览。
- **可独立调用的 CLI 子项目。** [CLI 子项目](cli/README.zh-CN.md)（`inferyard`）负责设备预检、主机互斥锁、冻结执行、证据封存、离线核验与报告投影，既在 Studio 底层自动运转，也支持终端与脚本直接调用。

## 开始使用

- **桌面测评**　从 [Releases](https://github.com/sunny0826/inferyard/releases/latest) 下载对应平台的 Studio。在 macOS 上打开 `.dmg` 并将 `InferYard Studio.app` 拖入 `Applications` 后，由于当前安装包未签名，首次启动前需在终端执行以下命令清除隔离属性：

  ```sh
  xattr -dr com.apple.quarantine /Applications/InferYard\ Studio.app
  ```

  启动后在「任务」页选择「准备环境」，再按 [Studio 首次使用说明](studio/README.md#pages-and-first-launch) 选择模型、引擎和题包。
- **终端测评**　从[安装与候选准备](cli/docs/installation.md)或 [CLI 说明](cli/README.zh-CN.md)开始。按候选参数启动服务，绑定实际 PID 和端点，分别运行题包并生成汇总报告。

## 现有题包

| 题包 | 内容 |
| --- | --- |
| `zh-core` | 120 道中文题：指令遵循、信息抽取、依据问答、数学与逻辑、分类和结构化输出，各 20 道 |
| `zh-svg-pelican` | 1 道 SVG 绘图题，原始证据保留生成内容，不评质量分 |
| `zh-smoke` | 40 道题（指令遵循和信息抽取），用于快速检查 |
| `zh-context-speed` | 3 段固定提示词，分别对应 64K / 128K / 256K 上下文容量，测量长上下文预填充与解码速度，不评质量分 |

常规测评分别运行 `zh-core` 和 `zh-svg-pelican`。每个题包发布前都经过人工审核，详见[题包说明](cli/bundles/README.md)。

## 支持平台

| 平台 | Studio 界面后端 | 测评与采集范围 |
| --- | --- | --- |
| macOS（Apple Silicon，12+） | Metal | 单次与批量运行、PolyStrata（Metal）与 `llama.cpp` / `mlx-lm` / `vLLM` / `SGLang`、CPU/RSS/换页及 AppleSMC 温度采集 |
| Windows x64（10/11） | Direct3D 11（支持 WARP 回退） | PolyStrata（CUDA/HIP）与 `llama.cpp` 串行文本运行、原生进程身份与内存采集 |
| Linux x64 | GTK 3 与 OpenGL（支持 CPU 渲染回退） | 单次与批量运行、PolyStrata（CUDA/HIP）与 `llama.cpp` / `vLLM` / `SGLang`、CPU/内存采集 |

表中列出实现范围。自动化原生桌面 UI 验收目前运行在 macOS（Apple Silicon）上，Windows 与 Linux 的原生窗口交互暂未纳入自动化 UI 测试覆盖。各平台准备方式见[安装指南](cli/docs/installation.md)。

## 设计原则与边界

- **本地优先。** 模型、运行记录与报告均保存在本机磁盘。InferYard 没有账号体系，也不会向外部上传任何结果。
- **受控的服务所有权。** Studio 仅启动并停止自己拉起的本机模型服务（从不自动重启），也支持在 PID 与启动时间核验后绑定你手动启动的本机服务。
- **明确的边界。** InferYard 不用模型当裁判，不把质量、速度和内存合成单一排名，不做多机调度，也不测试图像或音频模型。

## 文档

| 主题 | 链接 |
| --- | --- |
| 下载 Studio 安装包 | [GitHub Releases](https://github.com/sunny0826/inferyard/releases/latest) |
| Studio 各页面说明、多语言与构建方式 | [Studio 说明](studio/README.md) |
| CLI 子项目与命令行用法 | [CLI 说明](cli/README.zh-CN.md) |
| CLI 安装与候选准备 | [安装指南](cli/docs/installation.md) |
| 冻结题包说明 | [题包说明](cli/bundles/README.md) |
| 本地源码开发与调试命令 | [本地开发命令](tools/README.md) |
| 参与贡献 | [CONTRIBUTING.md](CONTRIBUTING.md) |
| 版本变更记录 | [变更记录](cli/CHANGELOG.md) |

## 致谢

感谢以下项目：

- [PolyStrata](https://github.com/simplexai-labs/PolyStrata)
- [llama.cpp](https://github.com/ggml-org/llama.cpp) / [PrismML-Eng/llama.cpp](https://github.com/PrismML-Eng/llama.cpp)
- [mlx-lm](https://github.com/ml-explore/mlx-lm)
- [vLLM](https://github.com/vllm-project/vllm) / [vllm-metal](https://github.com/vllm-project/vllm-metal)
- [SGLang](https://github.com/sgl-project/sglang)
- [LM Studio](https://lmstudio.ai)
- [MyGo](https://github.com/egoist/mygo) / [onevra](https://github.com/oneo-me/onevra)

## 许可证

[MIT](LICENSE)
