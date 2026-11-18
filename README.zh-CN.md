# InferYard

[English](README.md) | 中文

**InferYard Studio 是一款桌面应用，用来测评本机运行的模型并保留原始证据。**

选好模型和引擎，点击 **开始测评**，Studio 就会用一组固定题目测试模型，记录每道题的答案、耗时以及
CPU 和内存占用，并在同一个窗口内展示报告。

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## Studio 能做什么

- **一步准备。** 在任务页准备运行环境、工作区、结果目录和模型目录，随后选择测评题集与模型/引擎组合。
- **下载或登记模型与引擎。** 在下载页新建、取消和重试下载，或在设置页登记本机已有的安装声明。
- **由 Studio 管理引擎。** Studio 按需启动并核验模型服务，在退出时停止自己启动的服务，不会自行重启服务；也可绑定你手动启动的本机服务。
- **实时查看运行。** 测评进行时查看逐题进度，以及 CPU、内存和可用温度传感器的采样曲线。
- **阅读报告。** 测评完成后可直接在任务页查看本轮汇总并打开报告，或在报告页按模型浏览历史任务与报告；包含多次运行的报告可切换查看。
- **检查设备。** 查看并刷新硬件快照，无法测量的数值会直接标明原因。

## 为什么用 InferYard

- **本地优先。** 模型在你自己的电脑上运行，运行记录和报告保存在本机磁盘。InferYard
  没有账号体系，也不会把结果提交到任何地方。
- **数字可信。** 质量、延迟和内存分开展示，不合成一个“总分”。缺失的测量显示为缺失并附带原因，
  不会记为 0。
- **可重复。** 题目集合是冻结的，按固定规则评分，不用另一个模型当裁判。每次运行都保留
  原始证据，之后可以离线再次核验。

## 快速开始

1. 从[最新 Release](https://github.com/sunny0826/inferyard/releases/latest)
   下载适合你系统的 Studio：macOS 挂载 `.dmg`，Windows 运行 `.exe` 安装器，Linux 解压
   `.tar.gz` 压缩包（早期版本为压缩包）。macOS 上执行 `open "InferYard Studio.app"` 打开。
2. 首次启动时 Studio 打开任务页。点击 **准备环境**，Studio 会自动配置所需的命令行运行组件
   （已有安装时直接复用），并创建或复用工作区、结果目录和模型目录。首次准备运行时可能需要几分钟。
3. 在 **下载** 中下载模型和引擎，或在 **设置** 中登记本机已有的安装声明。
4. 回到任务页，选择模型、引擎和 **测评题集**，点击 **开始测评**。
5. 测评完成后在完成步骤点击 **查看报告**，或随时在 **报告** 中打开历史结果。

Studio 界面目前为中文。各页面说明见 [Studio 说明](studio/README.md)。

> Studio 构建尚未签名或公证，首次打开时系统可能会弹出警告。

## 现有题包

| 题包 | 内容 |
| --- | --- |
| `zh-core` | 120 道中文题：指令遵循、信息抽取、依据问答、数学与逻辑、分类和结构化输出，各 20 道 |
| `zh-svg-pelican` | 1 道 SVG 绘图题，只展示结果，不评分 |
| `zh-smoke` | 40 道题（指令遵循和信息抽取），用于快速检查 |
| `zh-context-speed` | 3 段长提示词（64K / 128K / 256K），测量长上下文速度，不评质量。测评类型选择 **长上下文速度** 并点击 **开始专项测评** |

常规测评同时运行 `zh-core` 和 `zh-svg-pelican`。每个题包发布前都经过人工审核，详见[题包说明](cli/bundles/README.md)。

## 支持平台

| 平台 | 系统要求 |
| --- | --- |
| macOS（Apple Silicon） | macOS 12+（Metal） |
| Windows x64 | Windows 10/11（Direct3D 11，支持 WARP 回退） |
| Linux x64 | GTK 3 和 OpenGL（支持 CPU 渲染回退） |

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
| 从源码构建开发 | [本地开发命令](tools/README.md) |
| 浏览全部文档 | [文档地图](cli/docs/README.md) |

## 许可证

[MIT](LICENSE)
