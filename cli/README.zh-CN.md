# InferYard CLI

[English](README.md) | 中文

`inferyard` 是 [InferYard Studio](../README.zh-CN.md) 的命令行执行子项目。Studio
提供桌面界面并管理模型服务，通过调用 `inferyard` 执行冻结题集、保存答案与延迟/资源原始证据，
并生成供 Studio 展示的 `studio.json` 报告。质量、等待时间和内存分别展示；比较条件不足时说明原因，
不合成一个总分。

## 安装

需要 Python 3.15（由 `uv` 自动托管安装）以及 `uv >= 0.13.6`：

```bash
uv tool install --managed-python --python 3.15 inferyard
inferyard --versions
inferyard --help
```

升级已有安装：

```bash
uv tool upgrade inferyard
```

## 常用命令速查

| 命令 | 用途 |
| --- | --- |
| `inferyard init --out DIR` | 初始化工作区并导出冻结题包（默认 `zh-core` + `zh-svg-pelican`，可通过 `--bundle` 选择 `zh-smoke` 或 `zh-context-speed`） |
| `inferyard device-check --json` | 只读设备预检并输出硬件感知首装推荐（PolyStrata 或 `llama.cpp`） |
| `inferyard engine prepare-polystrata` | 托管安装 PolyStrata（预检、独立 Python 3.10–3.13 `.venv`、运行时、模型下载与 `installation.json`） |
| `inferyard engine prepare` / `engine check` | 下载校验并解包固定版本引擎归档，或只读核验本机引擎与 `installation.json` |
| `inferyard engine detect` / `engine init-installation` | 探测本机引擎二进制或 Python 虚拟环境，并生成通过校验的 `installation.json` |
| `inferyard model acquire` / `model resolve` | 解析或下载固定版本的 Hugging Face / ModelScope 模型（支持单/多分片 GGUF 与目录模型、断点续传与代理） |
| `inferyard assets scan` | 只读扫描本地模型与引擎目录 |
| `inferyard service discover` | 只读发现本机回环地址上正在运行的推理服务（`/health`、`/v1/models`） |
| `inferyard plan` | 预览（`--dry-run`）或冻结离线实验计划（支持 `--preset context-speed` 用于长上下文速度测试） |
| `inferyard probe` / `run` / `resume` | 探测已绑定服务、执行单次/计划测评，或断点续跑未执行用例 |
| `inferyard verify` / `report` / `speed-results` | 离线核验封存 run、合并生成 `studio.json` 报告，或读取已核验的长上下文速度批次结果 |
| `inferyard compare` / `compare-engines` | 离线对比冻结测评证据或跨引擎对比批次 |
| `inferyard --schema <kind>` / `--versions` | 导出活动 JSON Schema（`schema_version = 3`）或运行时与依赖版本信息 |

## 从终端开始

1. 按[安装指南](docs/installation.md)安装 `inferyard`，创建操作目录并运行设备预检。
2. 按[平台步骤](docs/installation.md#3-准备候选)准备候选。在独立终端按候选参数启动模型服务，用实际 PID 和本机端点绑定。
3. 分别运行 `zh-core` 与 `zh-svg-pelican` 题包，核验各自的原始 run，再生成一份汇总报告。已有完整 `zh-core` 结果时，只补缺少的 `zh-svg-pelican` 运行。
4. 在 Studio 阅读 `studio.json`。交付时一并复制封存的 run 和报告（如需脱敏公开证据可使用 `inferyard public package`）。

桌面工作流从 [Studio 任务页](../studio/README.md#pages-and-first-launch) 开始。Studio 可以复用已有 CLI、一键升级 CLI，并管理自己启动的服务。

## 继续阅读

[InferYard Studio](../README.zh-CN.md) · [Studio 说明](../studio/README.md) ·
[安装指南](docs/installation.md) ·
[题包说明](bundles/README.md) ·
[贡献指南](CONTRIBUTING.md) · [变更记录](CHANGELOG.md)

CLI 开发命令在本仓库的 `cli/` 目录内执行；根级 `mise` tasks 已内置切换目录。项目采用 [MIT 许可证](LICENSE)。
