# InferYard

用本地模型完成冻结题目，保存答案、耗时和资源证据，再生成可离线阅读的 HTML 报告。
质量、等待时间和内存分别展示；比较条件不足时说明原因，不合成一个总分。

**v0.0.1 已发布到 [PyPI](https://pypi.org/project/inferyard/0.0.1/)，支持 uv / uvx 安装运行。**
项目采用 [MIT 许可证](LICENSE)。
产品为 Python CLI + 自包含 HTML，包名 `inferyard`，命令为 `inferyard`。

## 安装

从 PyPI 安装固定版本：

```bash
uv tool install --managed-python --python 3.14.7 inferyard==0.0.1
inferyard --help
```

也可直接通过 `uvx` 运行，无须持久安装：

```bash
uvx --isolated --managed-python --python 3.14.7 inferyard==0.0.1 --help
```

无需克隆源码或安装 mise。首次准备 Python 和依赖需要联网；模型和引擎由操作者另行准备。
[安装指南](docs/installation.md)包含 uv 安装、Release 附件安装、三平台资产准备、服务绑定及离线使用。
维护者的[候选构建与安装检查](CONTRIBUTING.md#本地候选构建与安装检查)绑定同一批发行物；
至少完成一个平台的实际安装检查，其余平台明确标为未验证。每个候选均须完成同批构建与实际安装检查。

## 最短工作流

完整 benchmark 默认覆盖 `zh-core` 和 `zh-svg-pelican`，仅在明确声明固定题包时单独运行指定题包，见[默认测评范围](docs/usage.md#默认测评范围)。下面是固定 `zh-smoke` 的快速检查示例。

```bash
inferyard init --out bench-work --bundle zh-smoke
inferyard device-check --model /path/to/model.gguf --out bench-work/preflight --json
```

首次实时操作先完成[主机初始化](#首次主机初始化)。
按[安装指南](docs/installation.md#3-准备候选)生成候选配置，在外部终端启动其中声明的模型服务，
将实际 PID 和端点代入：

```bash
inferyard config bind --candidate bench-work/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-work/bound
inferyard run --config bench-work/bound/config.toml
inferyard report --runs bench-work/results/RUN_ID --out bench-work/report
inferyard verify --path bench-work/report
```

`RUN_ID` 取自运行结果。用浏览器打开 `bench-work/report/report.html`；服务关闭后仍可阅读和核验。
CLI 连接操作者启动的本机服务；`run` 包含当次普通/流式探测，独立 `probe` 用于可选排障。

## 平台与能力

| 平台 | 实现范围 | 入口 |
| --- | --- | --- |
| Linux x64 | 单次、批量、CPU/内存采集；手工配置准备 | [安装](docs/installation.md#linux-x64) |
| Windows x64 | Prism/KVMem/NInfer 串行文本执行、原生身份与内存采集 | [Windows](WINDOWS.md) |
| macOS arm64 | 单次、批量、CPU/Metal 核验、CPU/RSS/换页采集 | [macOS](MACOS.md) |

[平台状态](docs/platforms.md)分别列出已实现、历史原生覆盖和当前候选未验项。
安装包能安装不等于所有引擎和传感器已通过原生验证。

题包覆盖指令遵循、提取、给定材料问答、数学逻辑、分类与结构化输出；SVG 题只展示生成结果。
评分采用冻结规则，不调用模型裁判。当前不包含社区账号、自动投稿、多机调度或多模态执行器。

## 继续阅读

[使用指南](docs/usage.md) · [CLI 参考](docs/cli-surface.md) · [报告](docs/reports.md) ·
[文档导航](docs/README.md) · [贡献指南](CONTRIBUTING.md) · [变更记录](CHANGELOG.md)

本仓库以 InferYard 当前源码建立全新 Git 历史。原始运行、模型资产与源项目旧 Git 历史不随仓库或安装包分发。
公开证据可通过 `public package` 生成脱敏包，结论范围见[证据血缘规则](docs/data-contract.md#证据血缘与比较结论)。

## 首次主机初始化

首次实时操作前执行 `inferyard host-state migrate`。它保存旧状态原字节、退休固定旧工具入口，
然后提交新状态；成功仍为 ready_to_run=false，运行前还要核验服务、资产和预算。
旧 dirty 先用原项目恢复；旧 lock-only 或凭据发布后来源变化需要调查，不能删锁或清空状态。
正常运行仅访问 inferyard-host.* 和不可变凭据。离线 verify/report 不建立主机状态。
历史格式和 origin=migrated 明确返回 unsupported_format/2；旧数据留在原项目处理。
Windows 原生迁移本次未验。详见[当前格式契约](docs/contracts/inferyard-current-format.md)。
