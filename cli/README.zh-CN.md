# InferYard

[English](README.md) | 中文

用本地模型完成冻结题集，保存答案、耗时和资源原始证据，并投影出供
[InferYard Studio](../studio/README.md) 桌面应用读取的单份 `studio.json` 报告。
质量、等待时间和内存分别展示；比较条件不足时说明原因，不合成一个总分。

**v0.0.4 已发布到 [PyPI](https://pypi.org/project/inferyard/0.0.4/)，支持 `uv` / `uvx` 安装运行。**
包名与命令均为 `inferyard`，采用 [MIT 许可证](LICENSE)。

## 安装

从 PyPI 安装固定版本：

```bash
uv tool install --managed-python --python 3.15.2 inferyard==0.0.4
inferyard --help
```

也可直接通过 `uvx` 运行，无须持久安装：

```bash
uvx --isolated --managed-python --python 3.15.2 inferyard==0.0.4 --help
```

无需克隆源码或安装 `mise`。首次准备 Python 和依赖需要联网；模型和引擎由操作者准备（也可通过
Studio 下载与管理）。[安装指南](docs/installation.md)包含 `uv` 安装、Release 附件安装、
三平台资产准备、服务绑定及离线使用。

## 最短工作流

常规测评默认覆盖 `zh-core` 和 `zh-svg-pelican`，仅在明确声明固定题包时单独运行指定题包（见
[默认测评范围](docs/usage.md#默认测评范围)）。下面以固定 `zh-smoke` 为例进行快速检查。

1. 初始化操作目录（`bench-ops/`）并执行设备预检：

```bash
inferyard init --out bench-ops --bundle zh-smoke
inferyard device-check --model /path/to/model.gguf --out bench-ops/preflight/device-1 --json
```

2. 按[安装指南](docs/installation.md#3-准备候选)生成候选配置，在独立终端自行启动其中声明的模型服务，
   再绑定实际 PID 和端点。CLI 从不自行启动、停止或重启模型服务：

```bash
inferyard config bind --candidate bench-ops/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-ops/bound/service-1
```

3. 运行测评并生成 `studio.json` 报告（`RUN_ID` 取自 `run` 的输出）：

```bash
inferyard run --config bench-ops/bound/service-1/config.toml
inferyard report --runs bench-work/results/RUN_ID --out bench-work/report
```

`run` 包含当次普通/流式探测，独立 `probe` 命令用于可选排障。主机状态在首次实时运行时自动创建，
不需要显式初始化步骤。

## 证据与目录布局

- **双目录隔离：** 准备检查、操作员脚本与日志存放在操作目录（`bench-ops/`），本轮 run 全部封存后可整体清理；
  测评工作区（`bench-work/`）只存放 run、batch 与报告，由 `run` 和 `report` 自动创建。对外交付时，
  将封存 run 与报告目录复制到独立交付目录，见[使用指南](docs/usage.md#工作区与交付目录组织)。
- **正式输出计数：** 源码开发版将一个完整普通 single run 保存为八份证据文件，报告为单份
  `studio.json`。现有合法 schema-3 快照运行保持只读支持，见
  [运行原件容器契约](docs/contracts/run-evidence-layout.md)。
- **证据权威：** `studio.json` 是供 [InferYard Studio](../studio/README.md) 读取的可重建投影，
  不是可核验产物。`verify` 检查原始 run，模型服务关闭后原始证据仍是唯一权威。

## 平台与能力

| 平台 | 实现范围 | 入口 |
| --- | --- | --- |
| Linux x64 | 单次与批量运行、CPU/内存采集；手工配置准备 | [安装](docs/installation.md#linux-x64) |
| Windows x64 | Prism/KVMem/NInfer 串行文本运行、原生身份与内存采集 | [Windows](WINDOWS.md) |
| macOS arm64 | 单次与批量运行、CPU/Metal 核验、CPU/RSS/换页采集 | [macOS](MACOS.md) |

- **验证状态：** [平台状态](docs/platforms.md)分别列出已实现、历史原生覆盖和当前候选未验项。
  安装包能安装不等于所有引擎和传感器已通过原生验证。维护者的
  [候选构建与安装检查](CONTRIBUTING.md#local-candidate-build-and-installation-checks)绑定同一批发行字节：
  至少一个平台完成实际安装检查，其余平台明确标为未验证。
- **题包与非目标：** 题包覆盖指令遵循、提取、给定材料问答、数学逻辑、分类与结构化输出；SVG 题只展示生成结果，
  `zh-context-speed` 测量长上下文预填充与解码速度。评分采用冻结规则，不调用模型裁判。社区账号、自动投稿、
  多机调度与多模态执行器不在范围内。
- **应用内存诊断：** macOS 与 Windows 提供 `inferyard memory-check`（见
  [应用内存诊断契约](docs/contracts/memory-check-contract.md)），输出进程内存与判断依据；Windows 区分
  working set 与 private commit，可执行文件分组保持保守 unknown 供用户确认用途。该命令不会关闭应用，
  也不保证退出后释放多少内存，自 PyPI `0.0.2` 构建起包含（`0.0.1` 构建不含）。

## InferYard Studio

[InferYard Studio](../studio/README.md) 是基于 MyGo 与 Go 构建的原生桌面面板。它调用 `inferyard`
CLI 完成环境准备、资源下载、引擎核验、测评运行和 `studio.json` 报告投影，并支持在首次启动时将同版本 CLI
安装到 `uv` 用户工具目录。

## 继续阅读

[使用指南](docs/usage.md) · [CLI 参考](docs/cli-surface.md) ·
[报告](docs/reports.md) · [文档导航](docs/README.md) ·
[贡献指南](CONTRIBUTING.md) · [变更记录](CHANGELOG.md)

CLI 开发命令在本仓库的 `cli/` 目录内执行；根级 `mise` tasks 已内置切换目录。

本仓库以 InferYard 当前源码建立全新 Git 历史；原始运行、模型资产与源项目历史不随仓库或安装包分发。
公开证据可通过 `public package` 生成脱敏包，结论范围见
[证据血缘规则](docs/data-contract.md#证据血缘与比较结论)。
