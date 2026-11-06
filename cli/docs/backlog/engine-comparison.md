# 跨引擎受控比较

[返回 Backlog](../backlog.md)

## 跨引擎受控比较

2026-11-06：[ADR 056](../decisions/056-engine-comparison.md) 与
[实施计划](../plans/engine-comparison.md) 已写入。同日实现单对 `compare --mode engine`
身份门槛和多块 `compare-engines`（调优声明、逐请求 token 配对、冷缓存、块交错与判定），
规则见[契约](../contracts/engine-comparison.md)。仅有合成数据单元回归；
未在真实 llama-server／LM Studio run 上执行，LM Studio 是否报告 `cached_tokens` 未知，
不报告时该侧阻断 `cold_cache_unverified`。结果未封存、无 HTML。
首组为同一 GGUF 下的 llama.cpp 对 LM Studio；多轮回放暂不做。
能力实测两次检查均因外部背景负载（代理进程约 160% CPU）温度超过 85°C 阈值
（96.9°C、95.5°C）而未发请求。
操作者已授权测评流程停止并重启 LM Studio 后台；原 PID 94330 在本轮检查时已不存在。

2026-11-06 真机：llama.cpp（Qwen3-0.6B Q8_0 GGUF）对 MLX-LM（同上游 8bit MLX），
Apple M4，engine 模式冻结 A/B×3，默认双题包，温度停止按操作者指示关闭（读数照常记录）。
两个题包的 `compare-engines` 均无阻断，范围 `engine_and_model_format`：
客户端解码间隔判 MLX-LM 更优（每 token 快 0.4–1.1 ms）；首个正文文本判 llama.cpp 更优
（zh-core 三对块快 8–12 ms）。E2E 与吞吐因输出长度不等仅展示。首内容延迟仅展示：
两侧计时的 token 不同（MLX-LM 计入 `<think>`）。
结果在 `.tools/engine-compare-mlx-20261106/compare-e4/`。
zh-svg-pelican 每块只有 1 题，证据弱。结论不归因于引擎实现，LM Studio 组合仍未实测。
`engine-comparison.json` 未封存、无 `verify` 与 HTML。
