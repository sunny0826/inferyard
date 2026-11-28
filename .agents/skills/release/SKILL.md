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

## 标准执行步骤（直接执行发版）

### 1. 确定目标版本并直接执行

默认目标为 `patch`，也可由用户指定 `minor`、`major` 或 `X.Y.Z`（若用户输入带 `v` 前缀如 `v0.0.11`，传参时去掉 `v`）。

收到用户明确发版请求后，**无需先展示 `--dry-run` 预览或二次确认**，直接在仓库根目录执行：

```sh
mise run release <target>
```

可选参数（按用户需求选用）：
- `--notes "..."`：覆盖自动收集的提交列表（可重复传入；`cli/CHANGELOG.md` 必须使用英文）
- `--dry-run`：仅在用户明确要求预演/预览发版内容时使用
- `--prepare-only`：仅在本地 `chore/release-vX.Y.Z` 分支完成改版、快检与提交，不 push、不调用 `gh`
- `--wait-pr-checks`：开 PR 后等待 PR 检查通过再执行 squash 合并
- `--no-watch`：推送 tag 后立即返回，不阻塞等待 `release.yml`

该命令使用本地仓库所有者 `gh` 账号自动完成：切出 `chore/release-vX.Y.Z` → 同步 4 处版本号与 `cli/CHANGELOG.md`（`cli/pyproject.toml`、`cli/src/inferyard/__init__.py`、`cli/uv.lock`、`cli/CHANGELOG.md`、`studio/mygo.config.ts`）→ 运行本地快检（`uv lock --check`、`ruff`、schema、`studio/scripts` 与 `tools` 单测）→ 提交并开 PR → `--squash --delete-branch` 合并回 `main` → 核对 `HEAD == mergeCommit.oid` → 推送 `vX.Y.Z` tag 并监听 `.github/workflows/release.yml`。

### 2. 前置阻塞项处理

若前置检查命中以下任一情况，立即停止并报告阻塞项，**不得擅自 `git reset`、`git stash`、丢弃未提交改动或强推**：
- 当前不在 `main` 分支
- 工作区存在未提交改动
- 本地 `main` 与 `origin/main` 未对齐
- 目标 tag `vX.Y.Z` 或分支 `chore/release-vX.Y.Z` 已存在
- 本地 `gh` 未登录仓库所有者账号

## 异常恢复与结果汇报

- **尚未发布前的 `release.yml` 失败（自愈并复用 Tag）**：若 `vX.Y.Z` tag 已推但 `release.yml` 因构建/测试问题失败，且 **GitHub Release 与 PyPI 均未发布（skipped / 未执行）**，直接自主完成恢复与重发，无需停下二次确认：
  1. 定位首个真实失败步骤并新建分支修复问题，跑完相关本地验证；
  2. 提交修复 PR，等待 PR CI 通过后 `--squash --delete-branch` 合入 `main`；
  3. 切回并同步 `main`，删除本地与远端同名 `vX.Y.Z` tag（`git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z`），在最新 `main` 上重新打 `vX.Y.Z` annotated tag 并推送，持续监听新的 `release.yml` 直到发布成功。
  4. 若仅为 CI 偶发抖动而非代码/测试问题，直接执行 `gh run rerun <run_id> --failed` 重跑并监听至成功。
- **已发布边界与其他阶段失败**：一旦 GitHub Release 或 PyPI **已经发布**，严禁覆盖已发布的 Release 或删除/强推重写已发布 tag；若在本地分支或发版 PR 阶段失败，按 `tools/release.py` 输出的 `Recovery:` 指引定位修复后继续完成发版。
- **发布成功**：简明汇报版本号、合并 PR 链接（含任何自愈修复 PR）、tag、GitHub Release 链接、PyPI 链接及 `release.yml` 运行结果。
