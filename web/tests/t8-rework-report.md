# T8 前端返工报告

基线：`e8cb5b6`。范围：仅 `web/**`，不修改契约、`internal/**`，不新增依赖。

## 改动清单

- `src/api/mock-runs.ts`：取消原因改为 `user_cancelled`，退出码仍为 130；停止原因与 limitations 分开。
- `src/api/run-schema.ts`：资源时间字段、可空 run_id 和嵌套进度数字字段对齐冻结镜像。
- `tests/verify-runs.mjs`：真实取消原因及 partial/dirty 不显示 cancelled 的断言。
- `tests/verify-runs-wire.mjs`：可空 run_id 的真实 SSE 接收、schema 边界及取消/时窗/预算展示断言。

## mock / wire 停止原因对照

| 场景 | 原 stop_reason | 修正后 |
| --- | --- | --- |
| 用户取消，退出 130 | interrupted | user_cancelled |
| 时窗中断，退出 3 | duration_requires_new_window | duration_window_interrupted |
| dirty 历史 / 退出 3 | engine_not_drained | duration_window_interrupted |
| 工具或证据错误，退出 4 | evidence_invalid | tool_interrupted |
| 预检阻断，退出 2 | device_preflight_blocked | null（未进入运行，无停止事件） |
| partial 历史样本 | null | duration_window_interrupted |
| 新增预算 wire 样本，退出 3 | — | single_wall_budget_exhausted / trial_wall_budget_exhausted |

`cli_status: interrupted` 保留；`engine_not_drained`、`evidence_invalid`、`device_preflight_blocked` 等仍作为原有 limitations。成功场景的 stop_reason 保持 null。扫描 web 全部 mock/wire，未留下上述伪造 stop_reason。

## S4 逐字段核对

唯一依据：冻结 `docs/api-types.ts`；未修改 `web/src/api/types.ts` 镜像。

| 类型 / 字段 | 冻结镜像 | 核对结论 |
| --- | --- | --- |
| ResourceSample.utc | string，必填 | datetime 校验放宽为 z.string()；接受空串和普通字符串。镜像不允许 null 或省略，因此不增加 nullable/optional |
| mem_available_bytes | number 或 null | 已一致 |
| service_rss_bytes | number 或 null | 已一致 |
| service_cpu_percent | number 或 null | 已一致 |
| temperature_celsius | number 或 null | 已一致 |
| RunProgressEvent.task_id | string | 已一致 |
| RunProgressEvent.run_id | string 或 null | 增加 nullable，并给事件 schema 添加镜像类型约束 |
| RunProgressEvent.progress | RunProgress，必填 | 嵌套逐字段核对如下 |
| RunProgress.phase | RunPhase 或 null | 七个枚举值一致 |
| done、last_seq | number | 去掉镜像未声明的 int/nonnegative 限制 |
| total | number 或 null | 去掉 int/nonnegative 限制，保留 null |
| current_case_id | string 或 null | 已一致 |
| cases | CaseProgress[] | 已一致 |
| CaseProgress.case_id | string | 已一致 |
| plan_index、duration_ms | number 或 null | 已一致 |
| state | CaseState | 六个枚举值一致 |
| category | ScoreCategory 或 null | 八个枚举值一致 |
| quality_state | QualityState 或 null | 四个枚举值一致 |
| started_at、finished_at、error_category | string 或 null | 已一致 |

新增回归验证空 utc / 普通字符串通过，null / 省略不通过；负小数进度数字依照镜像可解析。预算和时窗中断以退出 3 呈现不完整，不显示 cancelled；user_cancelled + 130 呈现已取消。

## 验证

- `bun run typecheck`：退出 0，svelte-check 0 errors / 0 warnings。
- `bun run build`：退出 0，230 modules transformed。

仓库根连续执行三次 `bun run web/tests/verify.mjs`，期间未修改产品代码或测试：

| 轮次 | 退出码 | 结果 | 完整日志 |
| --- | --- | --- | --- |
| 1 | 0 | 6 / 6 PASS，含新增断言 | `/tmp/inferyard-t8-b-verify-1.log` |
| 2 | 0 | 6 / 6 PASS，含新增断言 | `/tmp/inferyard-t8-b-verify-2.log` |
| 3 | 0 | 6 / 6 PASS，含新增断言 | `/tmp/inferyard-t8-b-verify-3.log` |

三轮均输出 `S4 schema parity; real cancellation/time-window/budget reasons`，并以 `no console exceptions` 完成。

本地提交为包含本报告的英文提交 `fix(web): align run fixtures and schemas with frozen contract`；确切哈希见交付消息。未 push。

测试范围为前端 mock 与 HTTP/SSE wire fixture，不代替并行服务端 B1 回归。
