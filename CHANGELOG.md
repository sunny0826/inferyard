# 变更记录

## Unreleased

### Removed

- Removed the `inferyard host-state migrate` command and the legacy host-state
  retirement mechanism (migration transactions, retirement markers and
  maintenance receipts).
- Legacy `local-ai-benchmark-host` lock/state files from the old tool are now
  completely ignored: they are never read, locked or retired, and InferYard no
  longer provides cross-tool mutual exclusion with the old tool.

### Changed

- Live entry points now create a clean host state automatically when acquiring
  the host lock for the first time; no explicit initialization step is needed
  on new machines.
- v0.0.1 host states carrying a `migration` envelope are accepted and the
  envelope is ignored; it disappears on the next state write. Leftover
  `inferyard-host-migration.json` receipt files are treated as unrelated files
  and left untouched.

## 0.0.1

首个公开分发版本。Python 包和 CLI 统一为 `inferyard`，许可证为 MIT，活动数据契约保持 `schema_version = 3`。
实际发布状态与附件见 [GitHub Releases](https://github.com/sunny0826/inferyard/releases)。

### 首次使用与旧数据

- 首次实时操作前运行 `inferyard host-state migrate`，显式初始化新主机状态或退休固定旧入口。
  已有 dirty 必须按原恢复流程处理；不能通过删除锁或重复迁移清空。
- 只接受[当前格式集合](docs/contracts/inferyard-current-format.md)。旧运行、旧报告格式及
  `origin=migrated` 明确拒绝；旧证据留在原项目处理，不在 InferYard 中转换。
- 安装验证采用 `installed_safe_checks.v3`，包含当前报告生成/核验与旧报告拒绝；旧安装结果不继承资格。

### 功能

- 执行冻结题包，保存逐题答案、确定性评分、客户端计时与平台资源证据。
- 支持单次与批量计划、取消与恢复、显式重跑；模型服务由操作者外部启动。
- 生成自包含离线 HTML 报告，提供比较、重评分、导出、脱敏公开包及证据核验。
- 随包提供题包、平台模板及 Windows 固定 runtime profile；支持 `init`、`config assets/create/bind`
  与 `runtime prepare`。模型、引擎、真实配置和原始运行不随安装包分发。
- 支持使用同批 wheel 和依赖约束进行 `uv tool` / `uvx` 安装；Windows 方法与指标目录显式按 UTF-8 读取。

### 分发与验证

- GitHub Actions 构建 wheel/sdist、从 sdist 重建并核对资源，执行 Linux x64、Windows x64 和 macOS arm64 安装检查。
- `community_distribution.v2` 候选绑定源码、构建与安装结果；核验后上传同一份字节并提供 `SHA256SUMS`。
- 发布 workflow 仅手动触发。`verify-only` 执行发布演练；`github-release` 创建带附件的草稿。
  PyPI 使用独立 Trusted Publisher 配置，本版本尚未上传 PyPI。

安装检查使用合成输入；当前版本的原生模型准备、完整题包及严格性能资格未由这些检查证明。
已验证范围与剩余项见[平台状态](docs/platforms.md)和[backlog](docs/backlog.md)。
