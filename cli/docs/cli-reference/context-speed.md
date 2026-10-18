# 长上下文速度专项

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「长上下文速度专项」的正文；内容不变，入口保留原节标题与锚点。

`init --bundle zh-context-speed --out NEW_DIR` 选择随包专项副本，默认双题库不变。
当前题库仍待人工审核，正式 init/create/plan 继续拒绝；隔离测试的受控审核响应不改变批准状态。

- `config create ... --profile context-speed --context-size 65536|131072|262144`：两个选项须同时提供。
  macOS 先刷新推荐再应用固定配方；Windows 保留 runtime receipt、设备选择和完整 argv 核验。
  模型 GGUF 的 context 上限不足或未知即拒绝，不降低安全阈值。
- `plan --preset context-speed --config BOUND_CONFIG --out NEW_PLAN`：与 `--experiment` 互斥；
  可用 `--dry-run` 替换 `--out`。复用既有冻结计划，五个 trial、正式请求上限 5。
  每个 trial 的双 probe 和一次预热另计，正常路径共 20 次生成请求；墙钟预算包含准备与排空。
- `run --plan NEW_PLAN/plan.json --output-root NEW_BATCH`：复用 batch 执行器，无自动重试。
  已有专项 batch（包括零 run）、专项 `resume`、`--rerun-from` 和 `run --config` 均拒绝。
  结果 details 返回 `next_results_command`，不生成通用 `repetition_metrics`。
- `speed-results --batch BATCH_DIR`：完整只读核验后输出 CommandResult，details 为
  `context_speed_result.v1`；`--schema context_speed_result` 查询结构。不会生成 HTML、
  Markdown、analysis、报告目录或缓存，也不发送请求。

读取退出码：执行完整 0（指标可缺测）、可读部分/取消后读取/零 run 为 3，
普通输入 2，损坏或来源矛盾 4。130 仅表示本次命令被取消。
字段和物理核验规则见[共享契约](../contracts/context-speed-contract.md)。
专项 progress 在准备阶段提示 profile/context，绑定后提示冻结计划，随后提示新 batch；
已有 batch 只推荐 `speed-results`，不推荐 resume/report 或旧 batch 续跑。
