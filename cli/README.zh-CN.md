# InferYard CLI

[English](README.md) | 中文

`inferyard` 是 [InferYard Studio](../README.zh-CN.md) 的命令行执行子项目。Studio
提供桌面界面并管理模型服务，通过调用 `inferyard` 执行冻结题集、保存答案与延迟/资源原始证据，
并生成供 Studio 展示的 `studio.json` 报告。质量、等待时间和内存分别展示；比较条件不足时说明原因，
不合成一个总分。

## 从终端开始

1. 按[安装指南](docs/installation.md)安装固定版本的 CLI，创建操作目录并运行设备预检。
2. 按[平台步骤](docs/installation.md#3-准备候选)准备候选。在独立终端按候选参数启动模型服务，用实际 PID 和本机端点绑定。
3. 分别运行 `zh-core` 与 `zh-svg-pelican` 题包，核验各自的原始 run，再生成一份汇总报告。已有完整核心结果时，只补缺少的鹈鹕运行。
4. 在 Studio 阅读 `studio.json`。交付时一并复制封存的 run 和报告。

桌面工作流从 [Studio 任务页](../studio/README.md#pages-and-first-launch)开始。Studio 可以复用已有 CLI，并管理自己启动的服务。

## 继续阅读

[InferYard Studio](../README.zh-CN.md) · [Studio 说明](../studio/README.md) ·
[安装指南](docs/installation.md) · [macOS](MACOS.md) · [Windows](WINDOWS.md) ·
[贡献指南](CONTRIBUTING.md) · [变更记录](CHANGELOG.md)

CLI 开发命令在本仓库的 `cli/` 目录内执行；根级 `mise` tasks 已内置切换目录。

本仓库以 InferYard 当前源码建立全新 Git 历史；原始运行、模型资产与源项目历史不随仓库或安装包分发。
公开证据可通过 `public package` 生成脱敏包。
项目采用 [MIT 许可证](LICENSE)。
