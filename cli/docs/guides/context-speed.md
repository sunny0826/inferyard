# 长上下文速度专项

本页是[使用指南](../usage.md) 的子文档，收录「长上下文速度专项」的正文；内容不变，入口保留原节标题与锚点。

本流程显式选择单一 `zh-context-speed`，不改变普通双题库默认范围。
**当前题库人工审核仍 pending，正式 init/create/plan 会拒绝。** 以下是审核满足后的操作顺序；
假引擎受控审核测试不代表正式题库已获批准或真实长上下文性能已验证。

```bash
inferyard init --bundle zh-context-speed --out speed-ops
inferyard device-check --model /path/to/model.gguf --json --out speed-ops/preflight/device-1
inferyard config create --workspace speed-ops --bundle zh-context-speed --engine /path/to/llama-server --profile context-speed --context-size 65536 --out speed-ops/candidates/one
# 在另一终端按 candidate.toml 的完整 startup_args 启动服务，再绑定实际 PID。
inferyard config bind --candidate speed-ops/candidates/one/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out speed-ops/bound/one
inferyard plan --preset context-speed --config speed-ops/bound/one/config.toml --out speed-plan-1
inferyard run --plan speed-plan-1/plan.json --output-root speed-ops/results/batch-1
inferyard speed-results --batch speed-ops/results/batch-1
```

候选例子适用于 macOS；Windows 用已核验的 `--runtime-receipt FILE` 替换 `--engine FILE`。
Linux 使用人工准备的合法绑定配置，从 plan 开始。CLI 不启动或停止服务。
context 仅选 65536、131072、262144，实际输入由对应模型模板后分词；不截断文本来凑档位。

保留每次 batch；失败或取消后可只读已有结果，重新测评新建完整 plan 和 batch。
不使用 resume、单 run 重跑或通用报告入口。输出只有 JSON；五次同条件有效测量才有汇总，
缺测原因和各次值始终保留。解释遵循[证据血缘规则](../data-contract.md#证据血缘与比较结论)。
语法与退出码见 [CLI 专项接口](../cli-surface.md#长上下文速度专项)，
字段见[共享契约](../contracts/context-speed-contract.md)。
