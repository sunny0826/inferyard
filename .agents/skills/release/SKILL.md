---
name: release
description: "Run InferYard's fast PR-compliant release workflow (`mise run release`) when the user explicitly asks to release, bump version, or preview a release of InferYard CLI and Studio. Do not trigger for routine commits, PR merges, or changelog edits outside a release request."
---

# Release

使用 `mise run release`（[tools/release.py](../../../tools/release.py)）执行 InferYard CLI 与 Studio 的统一发版流程。

## 适用与边界

- **触发范围**：仅在用户明确要求发版、预演发版或提升版本时启用；普通提交、合并 PR 或单独修改文档不得触发。
- **版本约束**：CLI 与 Studio 必须保持同一严格数字版本 `X.Y.Z`（Windows Inno Setup 要求，不支持 `v` 前缀或 `-rc` 后缀）。
- **不可逆边界**：推送 `vX.Y.Z` tag 后，`.github/workflows/release.yml` 会直接发布正式 GitHub Release 并推送至 PyPI（PyPI 已发布版本不可撤回或覆盖）。

## 标准执行步骤（必须先预览再确认）

### 1. 确认目标版本并只读预览

默认目标为 `patch`，也可由用户指定 `minor`、`major` 或 `X.Y.Z`。先运行只读预览：

```sh
mise run release <target> --dry-run
```

- `cli/CHANGELOG.md` 必须使用英文；如需润色或精简自上个 tag 起的提交摘要，先拟好英文要点并通过多个 `--notes "..."` 参数重新 `--dry-run` 预览。

### 2. 展示摘要并获取明确确认

向用户汇报：
- 版本变更：`current -> next`（及 tag `vX.Y.Z`）
- 将同步修改的 5 个文件：`cli/pyproject.toml`、`cli/src/inferyard/__init__.py`、`cli/uv.lock`、`cli/CHANGELOG.md`、`studio/mygo.config.ts`
- Changelog 预览条目
- `dry-run` 输出的任意前置警告

**必须等待用户明确同意后**，才可执行写操作与远端发布。

### 3. 检查前置阻塞项

若 `dry-run` 或前置检查提示以下任一情况，立即停止并报告阻塞项，**不得擅自 `git reset`、`git stash`、丢弃未提交改动或强推**：
- 当前不在 `main` 分支
- 工作区存在未提交改动
- 本地 `main` 与 `origin/main` 未对齐
- 目标 tag `vX.Y.Z` 或分支 `chore/release-vX.Y.Z` 已存在
- 本地 `gh` 未登录仓库所有者账号

### 4. 执行发版

获得用户明确确认后，在仓库根目录执行：

```sh
mise run release <target>
```

可选参数（按用户需求选用）：
- `--notes "..."`：覆盖自动收集的提交列表（可重复传入）
- `--prepare-only`：仅在本地 `chore/release-vX.Y.Z` 分支完成改版、快检与提交，不 push、不调用 `gh`
- `--wait-pr-checks`：开 PR 后等待 PR 检查通过再执行 squash 合并
- `--no-watch`：推送 tag 后立即返回，不阻塞等待 `release.yml`

该命令使用本地仓库所有者 `gh` 账号自动完成：切出 `chore/release-vX.Y.Z` → 同步 4 处版本号与 `cli/CHANGELOG.md` → 运行本地快检（`uv lock --check`、`ruff`、schema、`studio/scripts` 与 `tools` 单测）→ 提交并开 PR → `--squash --delete-branch` 合并回 `main` → 核对 `HEAD == mergeCommit.oid` → 推送 `vX.Y.Z` tag 并监听 `.github/workflows/release.yml`。

## 异常恢复与结果汇报

- **中途失败**：严格按 `tools/release.py` 输出的 `Recovery:` 指引说明停在哪个阶段（本地分支 / PR 已开 / PR 已合 `main`），不要覆盖已发布的 GitHub Release 或强推重写已发布 tag；若仅 CI 某一步偶发失败，可提示用户用 `gh run rerun <run_id> --failed` 重跑失败 job。
- **发布成功**：简明汇报版本号、合并 PR 链接、tag、GitHub Release 链接、PyPI 链接及 `release.yml` 运行结果。
