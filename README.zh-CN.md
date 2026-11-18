# InferYard

[English](README.md) | 中文

**InferYard 是一套以 InferYard Studio 桌面应用为核心、配套命令行执行子项目的本地大模型测评工具。**

它使用冻结的中文测评题集与长上下文速度测试，在本机运行的模型上执行测评，记录逐题答案、请求耗时与系统资源采样，并将原始证据保留在本地磁盘，生成可在应用内查看的原生报告。

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## 项目核心能力

- **确定性本地测评。** 使用人工审核后冻结的题集测试本地模型服务，按固定规则评分，不依赖其他模型充当裁判。
- **分维度可信指标。** 将回答质量、请求延迟、吞吐以及 CPU、内存和可用温度传感器的采样分开展示，不合成单一“总分”；无法测量的指标保持为 `null` 并标明原因，绝不记为 `0`。
- **可核验的原始证据。** 在本机持久化保存逐题输出、执行元数据与资源采样，支持离线核验，并投影出供 Studio 展示的 `studio.json` 报告。
- **Studio 桌面端管理。** [InferYard Studio](studio/README.md) 基于 Go 与 MyGo 构建原生桌面界面，提供设备硬件检查、模型与引擎下载或本机安装登记、模型服务生命周期管理、实时进度与资源曲线展示以及多维度报告阅读。
- **CLI 执行子项目。** [CLI 子项目](cli/README.zh-CN.md)（`inferyard`）负责设备预检、冻结执行、主机互斥锁、证据封存、离线核验与报告投影。Studio 在底层调用它完成测评，操作者也可在终端中独立运行。

## 项目结构

| 组件 | 职责 | 技术栈 |
| --- | --- | --- |
| [InferYard Studio](studio/README.md)（`studio/`） | 原生桌面应用，负责测评配置、资源下载与本机引擎管理、过程监控及报告浏览 | Go、MyGo、onevra |
| [InferYard CLI](cli/README.zh-CN.md)（`cli/`） | Studio 调用的命令行执行与证据子项目（也支持终端直调），负责预检、冻结运行、证据封存核验与 `studio.json` 生成 | Python |

## 现有题包

| 题包 | 内容 |
| --- | --- |
| `zh-core` | 120 道中文题：指令遵循、信息抽取、依据问答、数学与逻辑、分类和结构化输出，各 20 道 |
| `zh-svg-pelican` | 1 道 SVG 绘图题，仅展示生成结果，不评质量分 |
| `zh-smoke` | 40 道题（指令遵循和信息抽取），用于快速检查 |
| `zh-context-speed` | 3 段长提示词（64K / 128K / 256K），测量长上下文预填充与解码速度，不评质量分 |

常规测评同时运行 `zh-core` 和 `zh-svg-pelican`。每个题包发布前都经过人工审核，详见[题包说明](cli/bundles/README.md)。

## 支持平台

| 平台 | Studio 界面后端 | 测评与采集范围 |
| --- | --- | --- |
| macOS（Apple Silicon，12+） | Metal | 单次与批量运行、CPU/Metal 核验、CPU/RSS/换页及 AppleSMC 温度采集 |
| Windows x64（10/11） | Direct3D 11（支持 WARP 回退） | Prism/KVMem/NInfer 串行文本运行、原生身份与内存采集 |
| Linux x64 | GTK 3 与 OpenGL（支持 CPU 渲染回退） | 单次与批量运行、CPU/内存采集 |

各平台支持的引擎和验证状态不同。依赖测评结果之前，请查看[平台状态](cli/docs/platforms.md)、
[macOS](cli/MACOS.md) 和 [Windows](cli/WINDOWS.md)。

## 设计原则与边界

- **本地优先。** 模型、运行记录与报告均保存在本机磁盘。InferYard 没有账号体系，也不会向外部上传任何结果。
- **受控的服务所有权。** Studio 仅启动并停止自己拉起的本机模型服务（从不自动重启），也支持在 PID 与启动时间核验后绑定你手动启动的本机服务。
- **明确的边界。** InferYard 不用模型当裁判，不把质量、速度和内存合成单一排名，不做多机调度，也不测试图像或音频模型。

## 获取与文档

预构建的 Studio 安装包（macOS `.dmg`、Windows `.exe` 安装器与 Linux `.tar.gz` 压缩包；界面目前为中文，尚未签名公证）发布在 [GitHub Releases](https://github.com/sunny0826/inferyard/releases/latest)。

| 主题 | 文档 |
| --- | --- |
| Studio 各页面说明与构建方式 | [Studio 说明](studio/README.md) |
| 报告结构与指标口径 | [报告](cli/docs/reports.md) |
| CLI 子项目与命令行用法 | [CLI 说明](cli/README.zh-CN.md) |
| 本地源码开发与调试命令 | [本地开发命令](tools/README.md) |
| 全部文档索引 | [文档地图](cli/docs/README.md) |

## 许可证

[MIT](LICENSE)
