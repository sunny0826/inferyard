# AGENTS.md

默认使用简洁中文交流，代码标识符保持原样。README、CHANGELOG.md、Git 提交信息与 PR 使用英文。

## 仓库结构

| 目录 | 职责 |
| --- | --- |
| `cli/` | Python CLI（`inferyard`），负责测评执行、原始证据落盘、离线核验与 `studio.json` 报告生成（`schema_version = 3`） |
| `studio/` | MyGo 原生桌面应用（Go，独立 module `inferyard/studio`），消费 `core.Service` 与 `studio.json`，无前端构建步骤 |

CLI 源码位于 `cli/src/inferyard/`：业务模块不依赖 `cli/` 入口；帮助、版本与 schema 查询不得加载实时后端；安装态资源从包内读取，不依赖源码目录。遵循 Ruff 规则（Python 3.15，100 列），单文件尽量保持在 ~500 行以内。

## 环境与验证命令

版本由 [mise.toml](mise.toml) 固定，勿混用系统 Python 或全局 pip。CLI 命令在 `cli/` 目录执行，Studio 命令在 `studio/` 目录执行（根级 `mise` tasks 已内置目录切换）：

```bash
# CLI（在 cli/ 执行）
mise exec -- uv run --frozen pytest -q
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen python scripts/export_schemas.py --check
mise exec -- uv run --frozen python scripts/export_catalogue.py --check
mise exec -- uv run --frozen python scripts/export_community.py --check

# Studio（在 studio/ 执行）
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test -tags ui_debug . ./internal/pages -count=1

# macOS 原生 UI E2E（需已登录桌面与完整 Xcode，在仓库根执行）
mise run test:studio-ui
mise run test:studio-ui:full
```

修改 schema 或目录导出源后，运行 `scripts/export_schemas.py` / `scripts/export_catalogue.py` / `scripts/export_community.py` 重新生成，不要手改生成产物。

## 工程原则（以代码为准，避免文档驱动内耗）

- **代码即真相源**：不强制为功能改动补写 ADR、长篇契约或镜像子系统文档；仅在用户入口（README、安装指南）受影响时同步更新。
- **严谨性留在后端与 Schema，不泄漏为 UI 免责文案**：
  - 后端/数据层保持真实严谨：缺测保持 `null`（不补 `0`），不覆盖原始运行目录与证据，保留安全停止与进程所有权检查。
  - Studio UI 只清晰呈现数据、状态和必要操作引导，**不要在界面上平铺防御性免责声明或重复解释底层实现边界**；确需交代的指标口径统一收进折叠区或简短标签。
- **测试测行为与语义，不锁死文案字面**：测试验证数据完整性、状态流转与隔离边界，避免用整句防御性文案作为测试断言。
- **Studio 预览与正式共用页面代码**：预览实现与 fixture 仅进入 `ui_debug && !release` 构建，不执行真实 IO 或持久化用户配置；正式构建与 `ui_debug,release` 拒绝 `--debug-ui`。
- **安全与隐私底线**：凭据仅通过 `endpoint.api_key_env` 引用且落盘脱敏，不提交模型权重、引擎二进制、真实设备配置、缓存或凭据。不自动重启模型服务，不静默越过主机锁或身份校验。
- **避免过度防御**：不为极低概率的假设性边界堆砌复杂度；先确认实际场景与收益再扩展。

<!-- pluvy skill pointer -->
When adopting, configuring, or running pluvy, or triaging its findings,
first read `.agents/skills/pluvy/SKILL.md` and follow it.

## 交付规则

- 交付改动必须先建分支，不直接提交到 `main`；通过相关检查后保留在本地提交。
- 未经用户明确同意，不执行 `git push`、创建/合并/关闭 PR。PR 合并使用仓库所有者账号（GitHub 网页或本地 `gh` CLI），不允许 bot 作者提交。
- 报告变更时简要说明改动内容、实际运行的检查及未验证范围。
