# TASKS - UI 重构设置工作区目录

## 需求
- 统一工作区目录
- 移除设置页中的“路径设置”卡片，只保留并开放工作区目录的设置，其余目录都依赖工作区目录设置而非可以分开设置
- 移除设置页“下载目录名”卡片，采用默认值不可手动设置
- 新版本需要清理旧版本混乱的各种目录设置，将其统一到新设置，不兼容旧版本

## 设计定稿（节点 a 顾问审阅确认）
1. 统一目录布局：在正常模式下，所有工作目录均由 `workspace_root`（默认 `~/.inferyard/workspace`）派生：
   - 结果目录：`<workspace>/results`
   - 模型目录：`<workspace>/models`
   - 报告目录：`<workspace>/reports`
   - 引擎目录：`<workspace>/engines`（移除旧 `~/.inferyard/engine` 回退目录）
   - 首次运行未配置 `workspace_root`（`u.WorkspaceRoot == nil`）时，`workspaceRoot(u)` 返回默认路径 `~/.inferyard/workspace`，其余派生目录为 `nil`（保持现有首次运行门控语义，待 `init` / `Provision()` / 手动保存工作区目录后生效）。
   - `BoundPaths != nil`（`!release` 开发模式 `--dev-profile` / `--dev-assets`）下，所有派生目录入口均优先读取 `s.paths`（`Workspace`/`Results`/`Models`/`Engines`/`CLI`）。
2. `UserSettings` 清理：移除 `ResultsRoot`、`ModelsRoot`、`ModelDownloadName`、`EngineDownloadName`；加载旧 `settings.json` 时忽略这些遗留字段（不迁移、不兼容旧版本）；`UpdateSettings` 在任意模式下均拒绝上述 4 个键并返回 `invalid_request`（包括 `s.paths != nil` 白名单移除 `model_download_name` / `engine_download_name`）。
3. 首次落盘变更判定：`persistSettings` 按派生后的有效 `resultsRoot` / `modelsRoot` 及 `str(u.WorkspaceRoot) != str(s.settings.WorkspaceRoot)` 判定变更，避免首次初始化默认工作区时漏触发 `rebuild`、`RefreshAssets` 与 `RefreshDevice`。
4. 内部保留项：`InferyardPath` 保留为 `UserSettings` 内部字段供 CLI 自动安装/升级与测试注入使用，不再在设置页 UI 暴露；模型页 `ExtraModelsRoots` 与开发模式 `BoundPaths` 保持可用。
5. 下载目录名：移除设置页“下载目录名”卡片及配置持久化，始终采用默认自动命名（`SuggestDownloadSlug("model"|"engine", sourceURL)`）。
6. 执行约束：按工作流指令在当前分支单线串行执行，不建新分支与 worktree。

## 任务清单
- [x] **T01：统一后端工作区目录模型与清理旧目录/下载目录名设置（`studio/internal/core`）**
  - `types.go`：从 `UserSettings` 移除 `ResultsRoot`、`ModelsRoot`、`ModelDownloadName`、`EngineDownloadName`；`EffectiveSettings` 保持派生输出 `WorkspaceRoot`、`ResultsRoot`、`ModelsRoot`、`ReportsRoot`。
  - `readiness.go` / `service.go` / `bound_paths.go` / `defaults.go`：提供统一的感知 `s.paths` 的派生方法（`resultsRoot`、`modelsRoot`、`reportsRoot`、`enginesRoot`、`activeModelRoots`）；`EngineRoots` 移除 `~/.inferyard/engine` 回退；`Provision()` 仅保存 `WorkspaceRoot` 并创建 `<workspace>/{results,models,engines}`；`UpdateSettings` 拒绝 4 个已移除字段（含 `s.paths != nil` 路径）；修正 `persistSettings` 首次落盘变更判定。
  - 全量替换 `core` 中对 `u.ResultsRoot`/`u.ModelsRoot`/`ModelDownloadName`/`EngineDownloadName` 的直接访问：`download_names.go`、`downloads.go`、`downloads_stream.go`、`polystrata_setup.go`、`onboarding.go`、`aggregate.go`、`runs.go`、`reports.go`、`progress.go`、`registry.go`、`asset_management*.go`、`settings_extra.go`、`context_speed_identity.go`、`context_speed_prepare.go`、`context_speed_run.go`、`context_speed_store.go`、`local_candidates.go`、`local_runs.go`。
  - 更新 `studio/internal/core/*_test.go`（含 `readiness_test.go` 调整为先 `init` 后 `Provision()`），新增统一派生、旧字段忽略、旧 patch 拒绝、首次落盘触发刷新与切换工作区联动单测；运行 `go test ./internal/core/...`。
- [!] **T02：重构 Studio 设置页 UI、Preview、i18n 与页面单测（`studio/internal/pages` + `l10n`）**
  - `preparation.go` / `window.go` / `form_validation.go` / `browse.go`：移除“路径设置”卡片与“下载目录名”卡片（含 `BoundPaths` 分支）；非 `BoundPaths` 模式下仅保留并开放“工作区目录”卡片；清理 `cliPath`/`results`/`models`/`modelName`/`engineName` 与 `browseCLI`/`browseResults`/`browseModels` 等废弃代码。
  - `models_management.go`：将读取 `s.settings.Settings.ModelsRoot` 改为 `s.settings.Effective.ModelsRoot`，保留 `browse.title.models` 供模型页附加目录选择使用。
  - `studio/internal/l10n/locales/settings/{zh-Hans,en}.json`（及受影响 locale）：更新工作区目录卡片与未保存离开弹窗文案，清理已废弃的 `settings.paths.*`、`settings.cli.*`、`settings.download_names.*` key，确保中英文一致且无硬编码中文。
  - `preview_fixtures.go` / `preview_operations.go` / `preview_benchmark.go` / `preview_downloads.go`：适配统一工作区目录模型与默认下载目录名。
  - 更新 `studio/internal/pages/*_test.go`，运行 `mise run test:studio` 与 `mise run test:studio:preview`。
  - 未解决问题（第 5 轮 Reviewer 打回）：`studio/internal/pages/models_management.go:69` 的 `isExtraModelRoot` 已无生产调用方（页面已改为调用 `isExtraModelRootDir`），仅 `studio/internal/pages/settings_save_test.go:189` 仍调用旧函数，待删除死代码并将测试断言改为调用 `isExtraModelRootDir`。
- [ ] **T03：更新 E2E / UI 测试、文档与全链路验证及节点 (d) 终审**
  - 更新 `studio/tests/e2e/env.go`、`studio/tests/e2e/chain_test.go`、`studio/tests/ui/run_release.py`、`studio/tests/ui/test_run_release.py`、`studio/tests/ui/ReleaseUITests.swift`（及 `PreviewUITests.swift` 如下载名变更）。
  - 检查并更新受影响文档：`studio/README.md`、`README.md`、`README.zh-CN.md`。
  - 运行全量验证：`mise run test:studio`、`mise run test:studio:preview`、`mise run test:studio:e2e`、`python3 -m unittest studio/tests/ui/test_run_release.py`。
  - 向常驻 `PRD-Advisor` 发起节点 (d) 全局终审，输出最终三份交付。
