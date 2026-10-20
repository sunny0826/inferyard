# 本地五引擎与 Studio 剩余实施

目标：五引擎正式完成 `zh-core` 和 `zh-svg-pelican`，Studio 正常页面完成相同流程。
用户已允许先继续 UI；正式模型验收仍保留为独立工作。
决定与字段见 [ADR 055](../decisions/055-local-engine-formal.md)、
[契约](../contracts/local-engines.md)，当前状态见[backlog](../backlog/model-engine.md#本地五引擎正式适配)。

- 在授权设备按冻结正式预算逐引擎执行双题包，保留停止原件与完整分母；
  不将历史 64-token 诊断当作正式题包。已有两次温停，不自动重试；
  待温度条件满足后继续，LM 现有后台的跨 scope 替换须先取得管理授权。
- 桌面解锁后运行原生 UI E2E，验收目录模型双题包和 LM 外部重绑、失败重试与报告。
- 完成真实 Studio → CLI → 五种模型服务的验收；受控 core/页面回归不替代真机证据。
- 在不破坏既有 host dirty 原件的条件下完成真实 CLI／假引擎边界套件；不绕过主机状态检查。
- 推送、PR 与合并等待用户明确授权。
