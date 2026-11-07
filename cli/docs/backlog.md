# Backlog

更新于 2026-11-06。当前进度集中在本页；证据解释见[血缘规则](data-contract.md#证据血缘与比较结论)。

## 长上下文速度专项测评

正文见[长上下文速度专项测评](backlog/context-speed.md#长上下文速度专项测评)。

## InferYard Studio 后续工作

- 普通任务页及加载占位已移除旧英文页顶标题，进入时读屏播报「任务」；当前文档和原型名称同步。
  页面回归、Go vet 通过，macOS 原生模拟回归 `run-eaiay3ib` 6 项通过，运行中截图已核对。
  原型 HTML 已检查结构和本地链接；浏览器安全策略禁止 `file://`，本轮未复验其渲染。

- 遗留引擎停止的 PID 复用 TOCTOU 窗口可收紧（Windows OpenProcess 句柄钉住 / Linux pidfd）；
  当前由停止全程 25ms 身份重核收窄，本轮不实施。

- 2026-10-14：[Studio Design System](../../studio/docs/design-system.md) 已用于六页、详情和报告窗口外壳。
  DS-01 至 DS-09 均有实现与软件回归，全量 Go 测试、vet 和三平台编译通过；
  独立审查及修复增量复核均无阻塞发现，六条表单路径的重复提交公告缺口已修复并通过回归。
  实际覆盖见[全页面审查](../../studio/docs/design-system-audit.md)。
- 报告打开失败后，再次打开同一失败报告仍不会重新公告相同错误；可见错误保留。
  后续补此路径的重复操作公告与键盘回归（本轮六条表单修复不覆盖报告打开）。
- 按规范补三平台原生窗口、键盘、系统偏好、辅助技术及离线报告验收；
  历史内存布局场景和 PNG 目检不替代真窗口证据；固定布局矩阵已按[测试策略](../../docs/testing.md)删除。
- 测试精简时观察到：CLI 输入框聚焦后首次插入错误，内存 UI 测试会失焦；
  已有错误后的异步更新仍保留焦点。首次报错的原生窗口表现与原因待核查，本轮未改产品代码。
- Studio 真实 CLI／假引擎链已隔离测试主机锁；双题包 run、report、verify 均成功，
  后续 `progress` 读取 34.07 MiB 报告索引却被 preparation JSON 的 16 MiB 上限拒绝。
  需核对[报告读取器](../src/inferyard/config/workspace_progress.py)与报告契约，
  不通过缩小默认题包或放宽所有 preparation 输入限制规避；本轮未改生产读取逻辑。
- 测试精简后的完整原生 UI 验收两次未通过，现场观察到其他应用／系统设置窗口干扰；
  具体失败原因尚未完全确认。用户已选择暂缓，待桌面可独占时重跑 `mise run test:studio-ui:full`。
- Toast 位置随 onevra v0.2026.13 改为顶部居中，并已在 `pageShellOptions` 显式固定
  （`components.ToastTop`）。内存渲染测得顶部会遮挡页面顶部内容；macOS 模拟窗口 e2e
  `run-0u6hr6ty` 12 项通过，未见遮挡可交互控件。读屏播报内容与高对比度外观仍待原生验收，
  Windows/Linux 位置表现未验证。

- Studio CI 已有 `studio-check.yml`，macOS/Windows 执行模拟引擎边界 e2e（含编译及 `go test` 默认检查）；
  单测暂仅在 macOS 执行，Windows 单测的路径、权限位和时序假设仍待清理。
  原生窗口、系统通知及正式分发签名/公证仍需分别验收。
- macOS 系统通知权限：T10 验收唯一未通过项为通知实际到达；系统拦截通知权限弹窗，
  非产品缺陷。随仓库迁移继续挂账，待系统设置授权或后续签名后复验。

## 新功能：应用内存诊断

正文见[新功能：应用内存诊断](backlog/memory-diagnostics.md#新功能应用内存诊断)。

## InferYard 首次公开准备

正文见[InferYard 首次公开准备](backlog/release.md#inferyard-首次公开准备)。

### v0.0.2 发布记录

正文见[v0.0.2 发布记录](backlog/release.md#v002-发布记录)。

### PyPI uvx 真机验证

正文见[PyPI uvx 真机验证](backlog/release.md#pypi-uvx-真机验证)。

## 运行证据文件合并

正文见[运行证据文件合并](backlog/evidence-runtime.md#运行证据文件合并)。

### 范围收回

正文见[范围收回](backlog/evidence-runtime.md#范围收回)。

## 已完成的实现与验证

正文见[已完成的实现与验证](backlog/release.md#已完成的实现与验证)。

## 本轮：提速、轻量、去冗余、减过度门禁

正文见[本轮：提速、轻量、去冗余、减过度门禁](backlog/evidence-runtime.md#本轮提速轻量去冗余减过度门禁)。

### 本轮已完成

正文见[本轮已完成](backlog/evidence-runtime.md#本轮已完成)。

### 不排期

正文见[不排期](backlog/evidence-runtime.md#不排期)。

### 2026-10-10 追加：校验热路径预编译与重复校验消除

正文见[2026-10-10 追加：校验热路径预编译与重复校验消除](backlog/evidence-runtime.md#2026-10-10-追加校验热路径预编译与重复校验消除)。

## 交付目录组织（#28）

正文见[交付目录组织（#28）](backlog/delivery-layout.md#交付目录组织28)。

## 跨引擎受控比较

正文见[跨引擎受控比较](backlog/engine-comparison.md#跨引擎受控比较)。

## 模型获取简化

正文见[模型获取简化](backlog/model-engine.md#模型获取简化)。

## 引擎获取与闭环下载 e2e

正文见[引擎获取与闭环下载 e2e](backlog/model-engine.md#引擎获取与闭环下载-e2e)。

## 模型 e2e 三平台接入

正文见[模型 e2e 三平台接入](backlog/model-engine.md#模型-e2e-三平台接入)。
