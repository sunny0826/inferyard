# 跨引擎受控比较

[返回 Backlog](../backlog.md)

## 已有证据

- 2026-11-06 Apple M4 短请求诊断：两条输入分别为 13、675 tokens，每条输入在两侧的计数相同。llama-server 禁用缓存后报告零命中；LM Studio 未报告命中数，重复请求由 362 ms 降至 80–85 ms。原件位于 `.tools/engine-compare-probe-20261106/`。早先两次预检分别在 96.9°C、95.5°C 停止；后一次探测按当次授权只记录温度（98–110°C）。诊断与正式比较的缓存条件见 [ADR 056](../decisions/056-engine-comparison.md#修订2026-11-06能力实测后)。
- 历史六块实验中，第 3 块第 19 题时断电并按 `environment_safety_condition_changed` 停止；补齐的首组六块因冻结为 `side-by-side` 被比较器拒绝。以下记录来自另行冻结为 engine 模式的六块。

2026-11-06 真机：llama.cpp（Qwen3-0.6B Q8_0 GGUF）对 MLX-LM（同上游 8bit MLX），
Apple M4，engine 模式冻结 A/B×3，默认双题包，温度停止按操作者指示关闭（读数照常记录）。
两个题包的 `compare-engines` 均无阻断，范围 `engine_and_model_format`：
客户端解码间隔判 MLX-LM 更优（每 token 快 0.4–1.1 ms）；首个正文文本判 llama.cpp 更优
（zh-core 三对块快 8–12 ms）。E2E 与吞吐因输出长度不等仅展示。首内容延迟仅展示：
两侧计时的 token 不同（MLX-LM 计入 `<think>`）。
封存结果（含 HTML）在 `.tools/engine-compare-mlx-20261106/compare-e5/`，
两个题包 `verify --rerender` 均通过。
zh-svg-pelican 每块只有 1 题，证据弱。结论不归因于引擎实现，LM Studio 组合的正式六块比较仍未执行。

上述 HTML 和重渲染回执属于当时的比较格式。当前 CLI 已实现 `compare-plan`、按 plan/batch 派生单文件 JSON 和离线重算，见[当前契约](../contracts/engine-comparison.md)。历史六份计划不证明当前单份 12 槽位计划已通过真机验收。

## 剩余工作

- 同 GGUF 的 llama.cpp／LM Studio 正式比较待验；先验证固定长度输出能否成立，再裁决 engine 模式的 `strict_fixed_length`。块间温度等待的证据记录也待补，继续采用各次冻结的资源停止条件。
- Studio 比较任务编排、页面与模拟一致性、共享状态文案的归属、独立审查及单份计划真机验收见[剩余实施顺序](../plans/studio-experiment-architecture.md)。真实设备须逐槽换服务，再用当前 CLI 派生并核验。
