# 协议定义一览

本页是[扩展协议与受控工作流](../extensions-protocol.md) 的子文档，收录「协议定义一览」的正文；内容不变，入口保留原节标题与锚点。

`VALIDATORS` 与 `reduce_packet` 共同定义了支持的扩展定义集合（字段语义见 `schemas_extensions.py`）：

- `closed_concurrency.v1`：冻结题序在固定并发槽位上的闭集请求组。`run_closed` 执行、`reduce_closed` 归约，产出五终态计数、观察峰值并发、窗口秒数、N01 completed/s、N02 goodput/s、排空与完整性标志；`N03_token_ITL` 固定缺测。
- `native_tools.v1`：模型经原生 tool_calls 驱动的白名单 mock 函数链。`run_tool_case` 执行、`reduce_tools` 重算四项检查并给出通过率；`ToolStream` 独立组装 SSE 工具增量，与正式通路的 answer-only 解析器不共用代码。
- `total_observer_control.v1`：合成扩展日志的四臂观测开销对照。on 臂由 `total_control_runtime.Observer` 完整接 ExtensionJournal/采样/安全轨迹，off 臂零落盘；`assess_total` 归约。
- `total_observer_control.v2`：正式 `run_trial` 通路上的同构四臂对照，on 臂是真实封存 trial、off 臂是测量后落盘的 baseline；归约仍为 `assess_total`，另加 `verify_control_rows` 行级核验。
- `conditional_events.v1`：外部条件事件的离线导入定义，经 `reduce_import` 归约，不属于扩展执行协议，live 不生产。

各定义的当前格式地位与旧版本拒绝策略见 [当前格式契约](../../../cli/docs/contracts/inferyard-current-format.md) 的「扩展及开销」行。

## 工具链细节（native_tools）

`native_tools` 的执行与判定要点：

- 可调用函数白名单只有 `add` 与 `lookup` 两个进程内 mock，catalogue 是固定的 `{"alpha": "北京", "beta": "上海"}` fixture；任何其他函数名直接拒绝（`tool_not_allowlisted`），不触网络与外部进程。
- 回复语义由 `parse_reply` 强制：`finish_reason == "tool_calls"` 必须带非空 `tool_calls`，`finish_reason == "stop"` 必须无 tool_calls 且有字符串 content；usage 的 token 计数必须是非负整数。
- 循环把 assistant message 与每个 tool result 追加回 messages 再发起下一轮；tool_calls 增量经 `ToolStream` 按 index（0-7）拼接 id/name/arguments 片段，缺 `[DONE]`、finish 后仍有增量或 usage 重复都判证据错误。
- 超时保留已执行的调用记录与失败终态；预算超限、参数非合法 JSON、调用 id 重复或缺失都终止该 case 并记 `failed`，归约时 `reduce_tools` 对每行重算 `score_tool_case` 与已存 checks 比对。
