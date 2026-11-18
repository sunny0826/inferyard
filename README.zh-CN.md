# InferYard

[English](README.md) | 中文

**InferYard Studio 是一款桌面应用，用来测评你在自己电脑上运行的模型，并保留完整证据。**

选好模型和引擎，点击运行，Studio 就会用一组固定题目测试模型，记录每道题的答案、耗时以及
CPU 和内存占用，并在同一个窗口里以报告形式展示结果。

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## Studio 能做什么

- **一步准备。** 在任务页准备工作区，选择题包和模型/引擎组合。
- **下载模型和引擎。** 在下载页新建、取消和重试下载。
- **由 Studio 管理引擎。** Studio 启动所需的模型服务，并在 Studio 关闭时停止它；
  不会自行重启服务。
- **实时查看运行。** 测评进行时查看逐题进度和 CPU/内存曲线。
- **阅读报告。** 不离开应用即可浏览历史任务并打开每份报告；包含多次运行的报告可切换查看。
- **检查设备。** 查看硬件快照，无法测量的数值会显示原因。

## 为什么用 InferYard

- **本地优先。** 模型在你自己的电脑上运行，运行记录和报告保存在本机磁盘。InferYard
  没有账号体系，也不会把结果提交到任何地方。
- **数字可信。** 质量、延迟和内存分开展示，不合成一个"总分"。缺失的测量显示为缺失，
  不会记为 0。
- **可重复。** 题目集合是冻结的，按固定规则评分，不用另一个模型当裁判。每次运行都保留
  原始证据，之后可以再次核验。

## 快速开始

1. 从[最新 Release](https://github.com/sunny0826/inferyard/releases/latest)
   下载适合你系统的 Studio 并解压。macOS 上执行 `open "InferYard Studio.app"` 打开。
2. 首次启动时 Studio 会打开任务页。点击 **准备环境**，Studio 会安装所需的 InferYard CLI
   （缺少 uv 时一并下载），并创建结果目录、模型目录和工作区。首次准备需要下载 Python
   运行时，可能需要几分钟。
3. 在 **下载** 中获取模型和引擎。
4. 在任务页选择模型、引擎和测评题集，点击 **开始测评**。
5. 在 **报告** 中打开结果。

Studio 界面目前为中文。各页面说明见 [Studio 说明](studio/README.md)。

> Studio 构建尚未签名或公证，首次打开时系统可能会弹出警告。

## 现有题包

| 题包 | 内容 |
| --- | --- |
| `zh-core` | 120 道中文题：指令遵循、信息抽取、依据问答、数学与逻辑、分类和结构化输出，各 20 道 |
| `zh-svg-pelican` | 1 道 SVG 绘图题，只展示结果，不评分 |
| `zh-smoke` | 40 道题（指令遵循和信息抽取），用于快速检查 |
| `zh-context-speed` | 3 段长提示词（64K / 128K / 256K），测量长上下文速度，不评质量。测评类型选择 **长上下文速度** |

常规测评同时运行 `zh-core` 和 `zh-svg-pelican`。每个题包发布前都经过人工审核，详见[题包说明](cli/bundles/README.md)。

## 支持平台

| 平台 | 系统要求 |
| --- | --- |
| macOS（Apple Silicon） | macOS 12+ |
| Windows x64 | Windows 10/11 |
| Linux x64 | GTK 3 和 OpenGL |

各平台支持的引擎和验证状态不同。依赖测评结果之前，请查看[平台状态](cli/docs/platforms.md)、
[macOS](cli/MACOS.md) 和 [Windows](cli/WINDOWS.md)。

## InferYard 不做什么

- 不用模型当裁判。
- 不把质量、速度和内存合成一个排名。
- 不上传结果，也不提供社区账号。
- 不做多机调度，不测试图像或音频模型。

## 文档

| 我想… | 阅读 |
| --- | --- |
| 了解 Studio 各页面 | [Studio 说明](studio/README.md) |
| 看懂报告 | [报告](cli/docs/reports.md) |
| 用命令行测评 | [CLI 说明](cli/README.zh-CN.md) |
| 浏览全部文档 | [文档地图](cli/docs/README.md) |

## 许可证

[MIT](LICENSE)
