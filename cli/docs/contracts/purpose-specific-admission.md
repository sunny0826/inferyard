# 按用途分派与服务复用

[返回主参考](../data-contract.md)

## 按用途分派

[ADR035](../decisions/035-purpose-specific-admission.md) 定义新的离线比较、诊断准入、
评分继续、服务复用与逐题审核边界。

- comparison.json 的 format_version=4、definition=phase2.v3；旧 format1–3 不支持。
  plan/run 的 definition_versions.comparison 保持 phase2.v1。
  observed_differences 与 eligibility 分开：前者描述当前样本，后者仍为受控结论。
  总体质量应同题/同规则/同评分器/同分母且评分完整；性能仍需原逐指标资格。
- report_format_version=8 使用当前比较定义及根模板；题面、输出、参考答案各存一次。旧报告 1–7 不支持。
- 可选 environment_admission={definition: environment-admission.v2, required_fields: [...]}
  在配置 conditions 或 experiment 声明；字段限定 ac_online/profile/governor/epp/
  macos_power_policy。未声明保持旧正式执行准入；probe/diagnostic 仅记录差异。
- 已完成请求的 unscorable 不改变执行终态；无身份/存储/服务异常时继续下一请求。
  评分原因保留，最终 partial/code3；旧停止证据不补造后续请求或分数。
- 服务复用应 clean、非冷启动且加载条件/身份一致，当次核验有效配置。
  service-reuse.json 记录同进程或替换、缓存未知、当次预热次数。替换需旧进程退出。
- case_review_records 为可选新审核数组，每项 definition=case-review.v1、case_id、
  content_sha256、reviewer、reviewed_at、conclusion。哈希绑定 prompt/reference_answer/
  rules/category/task_protocol/answer_policy（含 case_id）。旧 review_records 保持旧哈希。
  未命中新记录的题应由仍匹配的旧整包签名/迁移证明覆盖，否则要求人工审核。


旧整包 content_hash 仍只排除 review_records。新增逐题记录也会改变整包身份；不伪造
旧哈希，不从失配的旧整包记录或活动内容快照推导逐题批准。旧 v1/v2 迁移证明继续严格
核验原内容等价。编辑后的新副本若不再满足迁移证明，应移除不适用的 review_provenance，
保留历史原件及原 review_records；新副本应有匹配的逐题记录或新的整包人工批准。

逐题摘要生成和人工填写示例见[使用说明](../usage.md#逐题人工审核)。摘要计算不是审核。

环境准入优先级为 experiment.environment_admission > conditions.environment_admission >
旧严格策略；identity.json 的 environment_admission 封存 policy_source、policy、原始差异
和应用后的 blockers。诊断仅豁免预检，显式 safety.check_environment 仍按冻结 conditions
触发运行期停止，不受 required_fields 或 diagnostic 覆盖。

跨 workload handoff 的同进程复用需主机非 dirty、旧 run 封存事件中的最后请求排空证明及当次 idle/身份/有效
配置核验。重跑不因工具版本、scorer 或运行身份与父运行不同而拒绝启动；比较记下这些差异，资格不成立。
未封存、不是单次运行、沿用同一服务进程，以及资产哈希、dirty、空闲、排空、温停和内存停，仍然拒绝。
评分失败或 partial 本身不是 dirty；缺证明返回 service_reuse_drain_evidence_missing。
排空摘要只随同次读取显式传递，不改变历史 read_trial 输出，也不缓存日志原始字节。
native HTTP 收据仅证明客户端完成，无法证明引擎排空，因此不能用于此复用路径。
同一冻结 workload 的串行 repeat/resume 沿用既有准入，不新增 engine_idle 要求。
native client_http 完成只允许原串行流程继续；不确定响应、dirty 或取消仍按原路径停止或
要求绑定恢复。service-reuse.json 的 serial_continuation 明示此范围，previous_drain
可为 null，不能据此声称已观察引擎排空。换进程仍核对旧进程退出；冷启动要求不豁免。

冷启动/加载实验用 `execution.require_fresh_process=true` 显式要求每次换进程。
批量模式每次只推进一个此类 trial，随后交接；缺此字段时 clean 同配置可复用。
复用不证明缓存冷却；service-reuse.json 或 service-binding.json 披露缓存 unknown 和预热次数。
