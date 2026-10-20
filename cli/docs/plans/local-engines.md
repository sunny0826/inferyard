# 本地五引擎与 Studio 剩余实施

目标：五引擎正式完成 `zh-core` 和 `zh-svg-pelican`，再接入 Studio 正常页面流程。
决定与字段见 [ADR 055](../decisions/055-local-engine-formal.md)、
[契约](../contracts/local-engines.md)。

- 在授权设备按冻结正式预算逐引擎执行双题包，保留停止原件与完整分母；
  不将历史 64-token 诊断当作正式题包。已有两次温停，不自动重试；
  待温度条件满足后继续，LM 现有后台的跨 scope 替换须先取得管理授权。
- 完成 CLI 后更新 Studio 引擎选择、候选描述、直接进程与 LM 所有权、
  队列、取消、重试和双题包报告范围。
- 同步 previewIntent 和生产编排 parity；分别验收页面、编排、原生窗口与真实模型。
- Studio 改动完成后执行对应最终检查并本地提交；推送仍待用户授权。
