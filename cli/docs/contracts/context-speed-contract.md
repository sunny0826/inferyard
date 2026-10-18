# 长上下文速度共享契约

本参考定义 A 阶段的纯规则、结构及语义校验。正式 CLI 投影与 Studio 消费属于后续接入；
结构通过不代表题库获人工批准、服务已核验或真实模型性能通过。
来源结论遵循[证据血缘规则](../data-contract.md#证据血缘与比较结论)，
格式支持遵循[当前格式契约](inferyard-current-format.md)。不接受历史格式迁移。

## 唯一 profile 与输入校验

唯一权威字段为可选 `experiment.profile="context-speed.v1"`。plan 内嵌 experiment，
整个字段自然参加既有 `plan_sha256`；普通计划不补字段，旧无 profile 计划的字节/hash 不变。
run、selection、batch 不另存独立 profile。`config.conditions.profile` 仍表示环境。

常量和纯规则位于 `config/context_speed_profile.py`：

| 常量 | 值 |
| --- | --- |
| `PROFILE` | `context-speed.v1` |
| `BUNDLE_ID` / `BUNDLE_VERSION` | `zh-context-speed` / `2026.10.1` |
| `BUNDLE_CONTENT_SHA256` | `a92e8b9f9e108071d979efc598c3ed55a047220a893b9a8c85525357e6d31407` |
| `CASES` | 65536→`context-speed-64k`，131072→`context-speed-128k`，262144→`context-speed-256k` |

固定一个 performance workload、一个 fixed case、五次重复、固定顺序、并发 1、自动重试 0，
`input_target_tokens=null`。输出 `strict_fixed_length=256`，不使用 prefix-reuse 协议或重复换序。
配置 repeats/concurrency/warmup_count 均为 1、slots=1、cache_policy=disabled，
`require_fresh_process` 不得为 true。`GENERATION` 固定温度 0、top_p/top_k 1、min_p 0、
presence_penalty 0、repeat_penalty 1、seed 0、seed_support=supported、reasoning off、空 stop。
seed 声明不代替运行时核验。

仅允许 `prism_llama_server_v1`；启动参数复用 `argument_values` 和 `disabled_startup`，
要求 context、parallel、reasoning 与配方一致，使用当前支持的封闭参数词表，拒绝投机、
draft/lookup/ngram 或未知选项。固定输出复用既有 `config.fixed_output.validate_inputs`。
Prism 实际构建仍须在 B 阶段通过现有 `/props` 的固定 `build_info` 核验，
release 显示字符串本身不证明构建已验证。

跨输入校验重新计算题库 `content_hash`，绑定正文、规则、答案政策与版本，不只认显示名。
专项 bundle/case 搭配缺失或错误 profile 必须拒绝；不能退回普通 performance。
该纯校验允许检查未批准草案，既有 `require_review` 及正式创建/运行审核关口保持原样。
当前批准数组为空；本轮没有注册社区资源或声称草案正式可运行。

接口：

```python
validate_experiment_profile(experiment) -> None
classify_workload_inputs(workload, config, bundle, *, experiment_profile=None)
# 返回 None（普通）或 "context-speed.v1"；矛盾抛 ContractError。
classify_verified_inputs(plan, config, bundle)
# 已核验来源的输出分派；专项仅一个 workload，普通行为保持。
validate_workload_inputs(workload, config, bundle, *, experiment_profile=None) -> None
```

这些接口要求先通过对应普通结构/语义校验。接线覆盖 `prepare_plan`、`read_frozen_plan`、
`ledger._inputs`。计划自身拒绝五个 trial 的重复顺序被改写；离线读取仍核对完整编译展开。
各通用消费者在 B 阶段接入共享 classifier，本阶段不新增 CLI 命令或输出旁路。

## 结果文档

注册键 `context_speed_result`；Schema 由 `scripts/export_schemas.py` 生成。
`CommandResult` 外壳没有 schema_version，`details` 是独立文档：
`schema_version=3`、`kind=context_speed_result.v1`。

| readable 字段 | 内容 |
| --- | --- |
| `read_state` / `profile` | `readable` / `context-speed.v1` |
| `source` | batch_metadata、plan、config、bundle 的 EvidenceRef；plan_sha256、experiment_id、workload_id |
| `metadata.model` | display_name、sha256、template_sha256 |
| `metadata.engine` | adapter、release、binary_sha256 |
| `metadata.bundle` | bundle_id、version |
| `metadata.selected_context_tokens` | 三档之一 |
| `execution` | counts（planned=5 和五终态计数）、stop_reason |
| `measurement` | status（complete/incomplete）、valid_count、reasons |
| `samples` | 固定五项，repeat_index=0..4，trial_id 唯一 |
| `summary` | 可空；五个 trial_ids 和 context/prompt_length/time/prefill/decode |

模型与引擎来自冻结配置，保留原任务身份；零 run 的 metadata 不声称服务已经验证。
当前 config 要求非空 display_name/release；缺失时属于坏输入，返回 error，不能临时采用
当前 UI 选择或同名扫描项。没有 Card/GPU/显存展示字段。

每个 Sample 包含 trial_id、repeat_index、case_id、可空 run、可空 request_id、
execution_state、可空 error_category、stop_reason、measurement、columns、proof。
存在的 run_id/request_id 各自唯一。run 包含 path、run_id、inputs EvidenceRef、
可空 manifest_sha256、integrity（sealed/unsealed）；sealed 必须有 manifest hash。
run=null 只允许 not_executed，不允许观测值、proof 或 cohort 身份。

`Value<T>={value,missing_reason,evidence_refs}`：null 必须有非空原因；非空不得有缺测原因，
且必须有引用。Context/Prompt length 是整数 token，Time 为秒，Prefill/Decode 为 token/s。
整数拒绝 bool；所有数值拒绝非有限值及负数。数字列不保存显示字符串。
零 run 必须保留五个 not_executed，停止 Value 为 null/`unknown`；不得捏造用户取消。

EvidenceRef 沿用现有 path/sha256/可选 member。路径相对 batch 根且不可穿越，run 引用位于
相应 run.path 下，文件 sha256 指实际文件字节。request 引用要求 `member.kind=request`
并匹配该 sample.request_id。其他成员沿用 input/observation；不新增 event member。
proof.final_timing 使用 `{file: EvidenceRef,event_seq: 正整数,record: ExistingEngineTimingRecord}`。
proof 另有 identity 数组、可空 token_budget、可空 request 和 fixed_output 数组。

纯校验可证明结构、引用归属和数值一致性，不能认证文件字节。B 读取器必须先核验 seal、
每个引用 hash、同一正式 request 的事件序号/身份/token budget/请求体/fixed-output 证据，
再投影；不得凭“有引用”把 build、seed、冷缓存或输出资格标为已验证。
相同文件内属于不同请求的事件必须在读取层拒绝，不能仅凭路径相同判为同一请求。

## 测量与汇总

`sample.measurement={valid,reasons,cohort_sha256}`。cohort_sha256 可空，它绑定已核验的同设备、
模型、引擎/计时定义、模板、配置、context、输入文本、缓存及输出条件；原始依据仍在 proof。
B 仅在这些条件都有可靠身份时生成稳定 cohort hash，排除 trial/run/request/PID 等每次变化
但不改变测量条件的标识。不能以两个 null 判定同条件；C 只消费，不自行构造该 hash。

`sample_reasons` 返回从投影即可判定的必要缺陷；生产者还必须附加实际来源核验的缺陷，
如 `engine_build_unverified`、`seed_support_unverified`、`fixed_output_unverified`。
valid 当且仅当 reasons 为空；已知必要缺陷不可被省略。未知 cohort 可以保留逐次有效值，
但整批 measurement 不完整。

Time=`prompt_ms/1000`。L06/L07 复用 `analysis.engine_timing.engine_rates`；不新增生产速度公式。
最终 timing 必须与 `capture_timings` 的当前记录语义一致。跨字段验证非空数值；缺少
proof.final_timing 时不能带旧速度。冷缓存汇总要求 cache_n=0、prompt_n+cache_n=实际输入，
输出达到 256，且 actual_input+256<=context。非正时长不提供对应有效速度。

汇总仅在五次有效、同一非空 cohort、同一 context 和 actual input 时存在。
summary 返回 time/prefill/decode 的 median/min/max，无 P95/最好值。
五次 token 不一致时可保留 valid_count=5，但 status=incomplete、summary=null，带汇总原因。
执行完整性独立于 measurement；完成但缺计时仍可退出 0，不将 completed 改成 failed。

## 校验接口与外壳

```python
validate_document("context_speed_result", details) -> None
# 内部调用 validate_result(data)；不要绕过结构检查直接使用该语义 hook。
validate_response(response, exit_code) -> None
validate_result_bindings(data, plan, runs) -> None
# runs: run_id -> 已核验的 run 文档；核对冻结计划、trial、run 归属，不执行 IO。
sample_reasons(sample, selected_context) -> list[str]
aggregate_reasons(samples) -> list[str]
```

| 情况 | status / completeness / exit |
| --- | --- |
| 执行完整（measurement 可缺测） | finished / complete / 0 |
| 可读部分、取消后离线读取、零 run | partial / incomplete / 3 |
| 非专项输入或参数错误 | error / incomplete / 2 |
| 文件、hash、归属或读取失败 | error / incomplete / 4 |

外壳固定 command=speed-results、run_id=null，readable 的 evidence_dir 指 batch。
执行完整必须由现有 plan/trial 完整性判断决定；`validate_response` 只检查其中可由投影
检查的必要条件，不以五个数值完整代替执行完整性。命令本身取消的 130 属命令层，
不是离线部分结果码。

error 分支严格仅有 schema_version、kind、read_state=error、error={code,source_path}。
不得混入 samples、summary、metadata 或旧缓存成绩。error.code 保留生产读取器的固定原因码，
不能包含原始异常、模型回复或凭据；source_path 可空。

## 固定原因码

输入规则的 ContractError.reason：
`unknown_profile`、`single_workload_required`、`fixed_order_required`、
`serial_without_retry_required`、`five_performance_repeats_required`、
`fixed_context_case_required`、`actual_input_required`、`fixed_output_required`、
`profile_protocol_conflict`、`profile_missing`、`bundle_identity_mismatch`、
`bundle_content_mismatch`、`context_unsupported`、`case_context_mismatch`、
`engine_unsupported`、`generation_recipe_mismatch`、`execution_recipe_mismatch`、
`cold_serial_required`、`cold_cache_startup_required`、`startup_recipe_mismatch`、
`startup_model_ambiguous`、`startup_value_missing`、`startup_option_ambiguous`、`startup_option_unsupported`、
`mixed_profile_sources`。普通契约原有异常仍原样保留。

逐次必要缺测/资格原因：`request_not_completed`、`sealed_run_required`、`request_missing`、
`identity_evidence_missing`、`token_budget_evidence_missing`、`request_evidence_missing`、
`fixed_output_evidence_missing`、`context_unverified`、`actual_input_unknown`、
`context_budget_exceeded`、`final_timing_missing`、`final_timing_unqualified`、
`cold_cache_unproven`、`input_token_mismatch`、`fixed_output_not_reached`、`metric_missing`。

汇总原因：`five_valid_samples_required`、`cohort_unknown`、`cohort_mismatch`、
`aggregate_context_mismatch`、`aggregate_prompt_length_mismatch`。
Value 原因沿用对应来源缺测码；零 run 停止原因固定 `unknown`。
具体结构/内部一致性拒绝码由 `context_speed.py` 提供，完整清单随 A 阶段 result.md 保存。

## 共享样例与验证边界

Python 与后续 Go 只消费同一份
[context_speed_contract.json](../../../studio/internal/inferyard/testdata/context_speed_contract.json)。
每项包含 name、valid、exit_code、完整 response；合法项同时核验结构、语义和外壳。
非法 JSON（重复 key、NaN/Infinity）单独以 raw 字符串测试，不写进共享 JSON。

样例使用独立常量与手算预期；缓存20/处理80/prompt400ms/生成11/decode500ms 得到
0.4/200/20，但不能进入冷缓存/256输出完整汇总。负例覆盖错误 profile/case、归属、
缺测原因、bool/负数、伪造汇总和 error 混入旧样本；Python 另覆盖非有限值和原始绑定。
测试数据不是模型性能证据，不执行服务 IO。

从 `cli/` 运行：

```bash
mise exec -- uv run --frozen pytest -q tests/unit/test_context_speed_contracts.py tests/unit/test_context_speed_profile.py
mise exec -- uv run --frozen python scripts/export_schemas.py --check
```

后续 B 需完成原始证据认证、cohort 构造、只读 JSON 投影及各通用出口拒绝；C 消费相同
Schema/黄金样例。Go、公开 CLI 五轮假引擎、原生 UI、真实模型和平台验收不由 A 的通过替代。
