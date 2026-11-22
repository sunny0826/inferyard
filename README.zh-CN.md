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
- **原生桌面工作台。** [InferYard Studio](studio/README.md) 基于 Go 与 MyGo 构建原生桌面界面，一站式完成设备硬件检查、模型与引擎下载或本机登记、服务启停管理、实时采样曲线监控与按模型汇总的报告浏览。
- **可独立调用的 CLI 子项目。** [CLI 子项目](cli/README.zh-CN.md)（`inferyard`）负责设备预检、主机互斥锁、冻结执行、证据封存、离线核验与报告投影，既在 Studio 底层自动运转，也支持终端与脚本直接调用。

## 开始使用

- **桌面测评**　从 [Releases](https://github.com/sunny0826/inferyard/releases/latest) 下载对应平台的 Studio。启动后在任务页选择「准备环境」，再按 [Studio 首次使用说明](studio/README.md#pages-and-first-launch) 选择模型、引擎和题包。
- **终端测评**　从[安装与候选准备](cli/docs/installation.md)开始。按候选参数启动服务，绑定实际 PID 和端点，分别运行题包并生成汇总报告。

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
| macOS（Apple Silicon，12+） | Metal | 单次与批量运行、CPU/Metal 核验、CPU/RSS/换页及 AppleSMC 温度采集 |
| Windows x64（10/11） | Direct3D 11（支持 WARP 回退） | Prism/KVMem/NInfer 串行文本运行、原生身份与内存采集 |
| Linux x64 | GTK 3 与 OpenGL（支持 CPU 渲染回退） | 单次与批量运行、CPU/内存采集 |

表中列出实现范围。v0.0.5 的 Windows 和 Linux 原生窗口交互仍待验收。各平台准备方式见[安装指南](cli/docs/installation.md)。

## 设计原则与边界

- **本地优先。** 模型、运行记录与报告均保存在本机磁盘。InferYard 没有账号体系，也不会向外部上传任何结果。
- **受控的服务所有权。** Studio 仅启动并停止自己拉起的本机模型服务（从不自动重启），也支持在 PID 与启动时间核验后绑定你手动启动的本机服务。
- **明确的边界。** InferYard 不用模型当裁判，不把质量、速度和内存合成单一排名，不做多机调度，也不测试图像或音频模型。

## 文档

| 主题 | 链接 |
| --- | --- |
| 下载 Studio 安装包 | [GitHub Releases](https://github.com/sunny0826/inferyard/releases/latest) |
| Studio 各页面说明与构建方式 | [Studio 说明](studio/README.md) |
| CLI 子项目与命令行用法 | [CLI 说明](cli/README.zh-CN.md) |
| CLI 安装与候选准备 | [安装指南](cli/docs/installation.md) |
| 本地源码开发与调试命令 | [本地开发命令](tools/README.md) |

## 许可证

[MIT](LICENSE)
